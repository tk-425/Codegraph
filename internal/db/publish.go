package db

import (
	"context"
	"fmt"
)

// RefreshPlan is the replacement unit of the graph one build attempt
// publishes. Extraction builds a candidate without touching the published
// graph; Publish applies the plan all-or-nothing (R05), so a reader sees
// the old snapshot or the new one, never a mixture, and a failure rolls
// everything back leaving the previous snapshot queryable.
type RefreshPlan struct {
	// RemovedFiles lose their symbols and metadata; nothing replaces them
	// (deleted, renamed, moved and newly ignored files).
	RemovedFiles []string
	// ChangedFiles' published symbols are deleted before Symbols — the
	// plan's complete symbol set for those files — is inserted.
	ChangedFiles []string
	Symbols      []*Symbol
	// DirtyLanguages have their whole edge sets replaced: every call, type
	// relationship and unresolved site belonging to the language is deleted
	// before the plan's edges are inserted. Dependency invalidation is not
	// proven safe, so a language touched by any file change re-extracts
	// all of its edges (R02).
	DirtyLanguages []string
	Calls          []*Call
	TypeHierarchy  []*TypeHierarchy
	Unresolved     []*Unresolved
	// FileMeta is upserted for the files this attempt re-extracted.
	FileMeta []FileMeta
	// ProviderState is upserted per language: the digest of the provider
	// configuration and capabilities the snapshot was extracted under.
	ProviderState map[string]string
	// ResetFormat replaces a graph held in an older index format. The
	// drop, recreate and version stamp run inside the publication
	// transaction, so a failed publication leaves the legacy snapshot
	// intact (R05, T23). Only an explicitly requested rebuild sets it.
	ResetFormat bool
	Attempt     *BuildAttempt
	Phases      []PhaseOutcome
	// ExpectedRevision rejects a candidate whose starting graph changed during
	// extraction, including changes by an older writer with no valid snapshot.
	ExpectedRevision *int64
	// PublishedSnapshot is assigned only after a successful commit.
	PublishedSnapshot *Snapshot
}

// Publish applies plan in a single transaction on the manager's write
// connection. Foreign-key enforcement is on for that connection, so edges
// are only inserted after every symbol they reference is in place.
func (m *Manager) Publish(ctx context.Context, plan *RefreshPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin publication: %w", err)
	}
	defer tx.Rollback()

	// A legacy graph is replaced only as part of a successful publication:
	// drop, recreate and stamp share the transaction, so a failure rolls
	// the legacy snapshot back whole (R05, T23).
	if plan.ResetFormat {
		for _, table := range graphTables {
			if _, err := tx.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
				return fmt.Errorf("failed to drop legacy %s: %w", table, err)
			}
		}
		for _, stmt := range AllSchemaStatements() {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("failed to recreate schema: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", FormatVersion)); err != nil {
			return fmt.Errorf("failed to stamp format version: %w", err)
		}
	}

	if plan.ExpectedRevision != nil {
		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM quality_state WHERE singleton = 1`).Scan(&revision); err != nil {
			return err
		}
		if revision != *plan.ExpectedRevision {
			return fmt.Errorf("published index changed during extraction; retry build")
		}
	}

	// Capture quality before write triggers invalidate it. The previous summary
	// is reusable only if no older writer has changed its graph since publication.
	previous, err := readSnapshot(tx.QueryRowContext(ctx, selectValidSnapshot))
	if err != nil {
		return fmt.Errorf("failed to read publication quality: %w", err)
	}

	// Edge sets are deleted through their endpoints, so this must run
	// before the endpoints' symbol rows are replaced.
	for _, language := range plan.DirtyLanguages {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM calls
			WHERE caller_id IN (SELECT id FROM symbols WHERE language = ?)
			   OR callee_id IN (SELECT id FROM symbols WHERE language = ?)`,
			language, language); err != nil {
			return fmt.Errorf("failed to replace calls of %s: %w", language, err)
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM type_hierarchy
			WHERE child_id IN (SELECT id FROM symbols WHERE language = ?)
			   OR parent_id IN (SELECT id FROM symbols WHERE language = ?)`,
			language, language); err != nil {
			return fmt.Errorf("failed to replace type relationships of %s: %w", language, err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM unresolved_relationships WHERE language = ?`, language); err != nil {
			return fmt.Errorf("failed to replace unresolved sites of %s: %w", language, err)
		}
	}

	// The file is the unit a refresh replaces symbols by. Removed and
	// changed files alike end with no rows before the new ones go in.
	replaced := make([]string, 0, len(plan.RemovedFiles)+len(plan.ChangedFiles))
	replaced = append(replaced, plan.RemovedFiles...)
	replaced = append(replaced, plan.ChangedFiles...)
	for _, file := range replaced {
		if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file = ?`, file); err != nil {
			return fmt.Errorf("failed to replace symbols of %s: %w", file, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM file_meta WHERE path = ?`, file); err != nil {
			return fmt.Errorf("failed to replace metadata of %s: %w", file, err)
		}
	}

	for _, s := range plan.Symbols {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO symbols (`+symbolColumns("")+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, s.Name, s.Kind, s.File, s.Line, s.Column, s.StartLine, s.StartColumn,
			s.EndLine, s.EndColumn, s.Scope, s.Signature, s.Documentation, s.Language,
			s.Source, s.CreatedAt); err != nil {
			return fmt.Errorf("failed to publish symbol %s: %w", s.ID, err)
		}
	}
	for _, c := range plan.Calls {
		if _, err := tx.ExecContext(ctx, insertCallSQL,
			c.CallerID, c.CalleeID, c.File, c.Line, c.Column, knownEvidence(c.Evidence)); err != nil {
			return fmt.Errorf("failed to publish call %s -> %s: %w", c.CallerID, c.CalleeID, err)
		}
	}
	for _, th := range plan.TypeHierarchy {
		if err := insertHierarchy(ctx, tx.ExecContext, th); err != nil {
			return fmt.Errorf("failed to publish type relationship %s -> %s: %w", th.ChildID, th.ParentID, err)
		}
	}
	for _, u := range plan.Unresolved {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO unresolved_relationships
			(kind, language, from_id, target_name, file, line, column, reason, candidates)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.Kind, u.Language, u.FromID, u.TargetName, u.File, u.Line, u.Column, u.Reason, u.Candidates); err != nil {
			return fmt.Errorf("failed to publish unresolved site %s %s:%d: %w", u.Kind, u.File, u.Line, err)
		}
	}
	for _, meta := range plan.FileMeta {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO file_meta (path, mod_time, language, content_hash, provider, extracted_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			meta.Path, meta.ModTime, meta.Language, meta.ContentHash, meta.Provider, meta.ExtractedAt); err != nil {
			return fmt.Errorf("failed to publish metadata of %s: %w", meta.Path, err)
		}
	}
	for language, digest := range plan.ProviderState {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO provider_state (language, digest, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)`,
			language, digest); err != nil {
			return fmt.Errorf("failed to publish provider state of %s: %w", language, err)
		}
	}

	snapshot, completed, err := publishQuality(ctx, tx, plan, previous)
	if err != nil {
		return fmt.Errorf("failed to publish quality: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit publication: %w", err)
	}
	plan.PublishedSnapshot = snapshot
	if completed != nil {
		*plan.Attempt = *completed
	}
	return nil
}
