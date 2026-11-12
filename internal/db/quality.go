package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// PhaseOutcome separates successful empty extraction, unavailable capability,
// fallback, and failed requests. Counts describe this provider's work, not totals.
type PhaseOutcome struct {
	Language       string `json:"language"`
	Phase          string `json:"phase"`
	Provider       string `json:"provider"`
	Status         string `json:"status"` // complete, fallback, unsupported, failed, reused
	Files          int    `json:"files"`
	Records        int    `json:"records"`
	FailedRequests int    `json:"failed_requests"`
	Reason         string `json:"reason,omitempty"`
}

type GraphCounts struct {
	Files             int            `json:"files"`
	Symbols           int            `json:"symbols"`
	Calls             int            `json:"calls"`
	TypeRelationships int            `json:"type_relationships"`
	Unresolved        map[string]int `json:"unresolved"`
}

// Snapshot describes the current graph, not a retained historical graph.
type Snapshot struct {
	ID             string         `json:"id"`
	PublishedAt    time.Time      `json:"published_at"`
	Quality        string         `json:"quality"` // complete or degraded; never whole-program completeness
	SourceDigest   string         `json:"source_digest"`
	ProviderDigest string         `json:"provider_digest"`
	Counts         GraphCounts    `json:"counts"`
	Phases         []PhaseOutcome `json:"phases"`
}

type AttemptDiagnostic struct {
	Language string `json:"language"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
}

// BuildAttempt survives a failed refresh independently of the last publication.
// A process interrupted without finalization remains running, never successful.
type BuildAttempt struct {
	ID          string              `json:"id"`
	StartedAt   time.Time           `json:"started_at"`
	FinishedAt  *time.Time          `json:"finished_at"`
	Outcome     string              `json:"outcome"` // running, succeeded, failed, cancelled
	SnapshotID  string              `json:"snapshot_id,omitempty"`
	Changed     bool                `json:"changed"`
	Phases      []PhaseOutcome      `json:"phases"`
	Diagnostics []AttemptDiagnostic `json:"diagnostics"`
	Error       string              `json:"error,omitempty"`
}

const createSnapshotTable = `CREATE TABLE IF NOT EXISTS current_snapshot (
 singleton INTEGER PRIMARY KEY CHECK(singleton = 1), data TEXT NOT NULL
);`
const createAttemptsTable = `CREATE TABLE IF NOT EXISTS build_attempts (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE, data TEXT NOT NULL
);`

// Validity belongs to the database, not the writer's executable version. Schema
// triggers also run for v0.9.0 writers, which do not know these reporting tables.
const createQualityStateTable = `CREATE TABLE IF NOT EXISTS quality_state (
 singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
 snapshot_valid INTEGER NOT NULL DEFAULT 0,
 attempt_valid INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO quality_state(singleton) VALUES(1);`

func qualityTriggerStatements() []string {
	var statements []string
	for _, table := range []string{"symbols", "calls", "type_hierarchy", "unresolved_relationships", "file_meta", "provider_state"} {
		for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
			statements = append(statements, "CREATE TRIGGER IF NOT EXISTS cg_quality_"+table+"_"+operation+" AFTER "+operation+" ON "+table+` BEGIN
 UPDATE quality_state SET snapshot_valid = 0, attempt_valid = 0, revision = revision + 1 WHERE singleton = 1;
END;`)
		}
	}
	for _, operation := range []string{"INSERT", "UPDATE"} {
		statements = append(statements, "CREATE TRIGGER IF NOT EXISTS cg_quality_attempt_"+operation+" AFTER "+operation+` ON build_attempts BEGIN
 UPDATE quality_state SET attempt_valid = 1 WHERE singleton = 1;
END;`)
	}
	return statements
}

const selectValidSnapshot = `SELECT s.data FROM current_snapshot s
 JOIN quality_state q ON q.singleton = s.singleton WHERE s.singleton = 1 AND q.snapshot_valid = 1`

func newRecordID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(id[:])
}

// SummarizePhases keeps per-file work bounded to phase/provider outcomes and
// gives persisted reports deterministic ordering.
func SummarizePhases(phases []PhaseOutcome) []PhaseOutcome {
	type key struct{ language, phase, provider, status, reason string }
	merged := map[key]PhaseOutcome{}
	for _, phase := range phases {
		k := key{phase.Language, phase.Phase, phase.Provider, phase.Status, phase.Reason}
		summary := merged[k]
		if summary.Phase == "" {
			summary = phase
		} else {
			summary.Files += phase.Files
			summary.Records += phase.Records
			summary.FailedRequests += phase.FailedRequests
		}
		merged[k] = summary
	}
	keys := make([]key, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.language != b.language {
			return a.language < b.language
		}
		if a.phase != b.phase {
			return a.phase < b.phase
		}
		if a.provider != b.provider {
			return a.provider < b.provider
		}
		if a.status != b.status {
			return a.status < b.status
		}
		return a.reason < b.reason
	})
	result := make([]PhaseOutcome, 0, len(keys))
	for _, k := range keys {
		result = append(result, merged[k])
	}
	return result
}

func NewBuildAttempt() *BuildAttempt {
	return &BuildAttempt{ID: newRecordID(), StartedAt: time.Now().UTC(), Outcome: "running", Phases: []PhaseOutcome{}, Diagnostics: []AttemptDiagnostic{}}
}

func (m *Manager) RecordAttempt(ctx context.Context, attempt *BuildAttempt) error {
	data, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO build_attempts(id, data) VALUES(?, ?)
 ON CONFLICT(id) DO UPDATE SET data = excluded.data`, attempt.ID, string(data))
	return err
}

func (m *Manager) LatestAttempt() (*BuildAttempt, error) {
	for _, table := range []string{"build_attempts", "quality_state"} {
		present, err := m.tableExists(table)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, nil
		}
	}
	var data string
	if err := m.db.QueryRow(`SELECT a.data FROM build_attempts a JOIN quality_state q ON q.singleton = 1
 WHERE q.attempt_valid = 1 ORDER BY a.sequence DESC LIMIT 1`).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var attempt BuildAttempt
	if err := json.Unmarshal([]byte(data), &attempt); err != nil {
		return nil, err
	}
	return &attempt, nil
}

// PublicationRevision detects writes by any executable during extraction. It
// complements validity: an already-invalid graph may change again before publish.
func (m *Manager) PublicationRevision() (int64, error) {
	var revision int64
	err := m.db.QueryRow(`SELECT revision FROM quality_state WHERE singleton = 1`).Scan(&revision)
	return revision, err
}

func (m *Manager) Snapshot() (*Snapshot, error) {
	for _, table := range []string{"current_snapshot", "quality_state"} {
		present, err := m.tableExists(table)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, nil
		}
	}
	return readSnapshot(m.db.QueryRow(selectValidSnapshot))
}

func readSnapshot(row *sql.Row) (*Snapshot, error) {
	var data string
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// publishQuality runs exclusively through the publication transaction. Even its
// counts and manifests are read from that transaction's post-replacement graph.
func publishQuality(ctx context.Context, tx *sql.Tx, plan *RefreshPlan, previous *Snapshot) (*Snapshot, *BuildAttempt, error) {
	snapshot := previous
	changed := previous == nil || plan.ResetFormat || len(plan.ChangedFiles) > 0 || len(plan.RemovedFiles) > 0 || len(plan.DirtyLanguages) > 0
	if changed {
		snapshot = &Snapshot{ID: newRecordID(), PublishedAt: time.Now().UTC(), Quality: "complete", Phases: plan.Phases}
		// Undirty languages retain their published extraction quality and limitations.
		if previous != nil {
			dirty := map[string]bool{}
			for _, language := range plan.DirtyLanguages {
				dirty[language] = true
			}
			for _, phase := range previous.Phases {
				if !dirty[phase.Language] {
					snapshot.Phases = append(snapshot.Phases, phase)
				}
			}
		}
		snapshot.Phases = SummarizePhases(snapshot.Phases)
		for _, phase := range snapshot.Phases {
			if phase.Provider == "tree-sitter" || phase.Status == "failed" || phase.Status == "unsupported" || phase.Status == "fallback" {
				snapshot.Quality = "degraded"
			}
		}
		for _, count := range []struct {
			table  string
			target *int
		}{
			{"file_meta", &snapshot.Counts.Files}, {"symbols", &snapshot.Counts.Symbols},
			{"calls", &snapshot.Counts.Calls}, {"type_hierarchy", &snapshot.Counts.TypeRelationships},
		} {
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+count.table).Scan(count.target); err != nil {
				return nil, nil, err
			}
		}
		snapshot.Counts.Unresolved = map[string]int{}
		rows, err := tx.QueryContext(ctx, `SELECT reason, COUNT(*) FROM unresolved_relationships GROUP BY reason ORDER BY reason`)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var reason string
			var n int
			if err := rows.Scan(&reason, &n); err != nil {
				rows.Close()
				return nil, nil, err
			}
			snapshot.Counts.Unresolved[reason] = n
			if reason != "external" && n > 0 {
				snapshot.Quality = "degraded"
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
		snapshot.SourceDigest, err = manifestDigest(ctx, tx, `SELECT path, content_hash FROM file_meta ORDER BY path`)
		if err != nil {
			return nil, nil, err
		}
		snapshot.ProviderDigest, err = manifestDigest(ctx, tx, `SELECT language, digest FROM provider_state WHERE language IN (SELECT DISTINCT language FROM file_meta) ORDER BY language`)
		if err != nil {
			return nil, nil, err
		}
		data, err := json.Marshal(snapshot)
		if err != nil {
			return nil, nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO current_snapshot(singleton, data) VALUES(1, ?)`, string(data)); err != nil {
			return nil, nil, err
		}
	}
	var completed *BuildAttempt
	if plan.Attempt != nil {
		copy := *plan.Attempt
		completed = &copy
		now := time.Now().UTC()
		completed.FinishedAt, completed.Outcome, completed.SnapshotID, completed.Changed = &now, "succeeded", snapshot.ID, changed
		data, err := json.Marshal(completed)
		if err != nil {
			return nil, nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO build_attempts(id, data) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET data = excluded.data`, completed.ID, string(data)); err != nil {
			return nil, nil, err
		}
	}
	// Revalidate only after graph + report + attempt writes all succeeded. Old
	// writers never execute this step, so their changed graph remains queryable
	// but cannot inherit a newer writer's quality/publication claims.
	if _, err := tx.ExecContext(ctx, `UPDATE quality_state SET snapshot_valid = 1 WHERE singleton = 1`); err != nil {
		return nil, nil, err
	}
	return snapshot, completed, nil
}

func manifestDigest(ctx context.Context, tx *sql.Tx, query string) (string, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hash := sha256.New()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return "", err
		}
		// Length framing avoids ambiguous concatenations, including unusual path bytes.
		fmt.Fprintf(hash, "%d:%s%d:%s", len(key), key, len(value), value)
	}
	return hex.EncodeToString(hash.Sum(nil)), rows.Err()
}

func knownEvidence(evidence string) string {
	if evidence == "" {
		return "unknown"
	}
	return evidence
}

// evidenceColumn is read-only schema adaptation for an unextended format-2
// database. Absence is unknown historical evidence, not incompatible identity.
func (m *Manager) evidenceColumn(table, alias string) (string, error) {
	present, err := m.columnExists(table, "evidence")
	if err != nil {
		return "", err
	}
	if !present {
		return "'unknown'", nil
	}
	return alias + ".evidence", nil
}

func (m *Manager) columnExists(table, column string) (bool, error) {
	rows, err := m.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	present := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var dflt any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			present = true
		}
	}
	return present, rows.Err()
}

// Do not impose a UNIQUE constraint that would reject an older writer's plain
// duplicate INSERT. New writers coalesce evidence inside their write transaction;
// historical duplicate rows remain intact and query presentation groups them.
func insertHierarchy(ctx context.Context, exec func(context.Context, string, ...any) (sql.Result, error), th *TypeHierarchy) error {
	evidence := knownEvidence(th.Evidence)
	result, err := exec(ctx, `UPDATE type_hierarchy SET evidence =
 CASE WHEN instr(',' || evidence || ',', ',' || ? || ',') > 0 THEN evidence ELSE evidence || ',' || ? END
 WHERE child_id = ? AND parent_id = ? AND relationship = ?`, evidence, evidence, th.ChildID, th.ParentID, th.Relationship)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated > 0 {
		return nil
	}
	_, err = exec(ctx, `INSERT INTO type_hierarchy(child_id, parent_id, relationship, evidence) VALUES(?, ?, ?, ?)`, th.ChildID, th.ParentID, th.Relationship, evidence)
	return err
}

// Only Initialize's build path adds optional provenance columns.
func (m *Manager) addMissingEvidenceColumns() error {
	for _, table := range []string{"calls", "type_hierarchy"} {
		present, err := m.columnExists(table, "evidence")
		if err != nil {
			return err
		}
		if !present {
			if _, err := m.db.Exec("ALTER TABLE " + table + " ADD COLUMN evidence TEXT NOT NULL DEFAULT 'unknown'"); err != nil {
				return err
			}
		}
	}
	return nil
}

// sqlConnection lets a pinned query view reuse existing query methods without
// allowing those methods to escape its transaction onto another pooled connection.
type sqlConnection interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
	Exec(string, ...any) (sql.Result, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	SetMaxIdleConns(int)
	Close() error
}

type readView struct {
	sqlConnection
	tx  *sql.Tx
	ctx context.Context
}

func (v *readView) Query(q string, args ...any) (*sql.Rows, error) {
	return v.tx.QueryContext(v.ctx, q, args...)
}
func (v *readView) QueryRow(q string, args ...any) *sql.Row {
	return v.tx.QueryRowContext(v.ctx, q, args...)
}
func (v *readView) Exec(q string, args ...any) (sql.Result, error) {
	return v.tx.ExecContext(v.ctx, q, args...)
}
func (v *readView) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return v.tx.ExecContext(ctx, q, args...)
}
func (v *readView) Close() error { _ = v.tx.Rollback(); return v.sqlConnection.Close() }

// PinRead keeps every subsequent query on one read transaction until Close.
// Call only on a mode=ro Manager, whose default BEGIN is deferred, never IMMEDIATE.
func (m *Manager) PinRead(ctx context.Context) error {
	if !m.readOnly {
		return fmt.Errorf("pinned query view requires a read-only database")
	}
	if _, pinned := m.db.(*readView); pinned {
		return fmt.Errorf("query view already pinned")
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	m.db = &readView{sqlConnection: m.db, tx: tx, ctx: ctx}
	// Establish the read snapshot now, before any live-LSP/text supplementation.
	_, err = m.Snapshot()
	return err
}
