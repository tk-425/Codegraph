package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

var implementationsLangFlag string

var implementationsCmd = &cobra.Command{
	Use: "implementations <interface>", Short: "Find implementations of an interface",
	Long: `Find types that implement or extend the specified interface.
Stored results describe the published snapshot; live LSP fallback is labelled separately.

Examples:
  codegraph implementations Reader
  codegraph implementations Service --lang=go`,
	Args: cobra.ExactArgs(1), RunE: runImplementations,
}

func init() {
	implementationsCmd.Flags().StringVar(&implementationsLangFlag, "lang", "", "Filter by language(s), comma-separated")
	supportJSON(implementationsCmd)
	rootCmd.AddCommand(implementationsCmd)
}

type implementationRecord struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	ID       string   `json:"id,omitempty"`
	Source   string   `json:"source"`
	Origin   string   `json:"origin"`
	Evidence []string `json:"evidence"`
}

func runImplementations(cmd *cobra.Command, args []string) error {
	name := args[0]
	emitErr := func(code string, err error, metadata *OutputMetadata) error {
		if jsonOutputFlag {
			if writeErr := EmitJSON(cmd.OutOrStdout(), "implementations", &name, []implementationRecord{}, []EnvelopeError{{Code: failureCode(code, err), Message: err.Error()}}, metadata); writeErr != nil {
				return writeErr
			}
		}
		return err
	}
	cwd, cfg, manager, code, err := openProject(commandContext(cmd))
	if err != nil {
		return emitErr(code, err, nil)
	}
	defer manager.Close()
	metadata, err := queryMetadata(manager)
	if err != nil {
		return emitErr("implementations_lookup_failed", err, nil)
	}
	var languages []string
	if implementationsLangFlag != "" {
		languages = strings.Split(implementationsLangFlag, ",")
	}
	records, live, err := lookupImplementations(commandContext(cmd), cmd, cwd, cfg, manager, name, languages)
	metadata.Live = live
	if err != nil {
		return emitErr("implementations_lookup_failed", err, metadata)
	}
	if jsonOutputFlag {
		return EmitJSON(cmd.OutOrStdout(), "implementations", &name, records, nil, metadata)
	}
	if err := printQueryMetadata(cmd, manager); err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "🔧 No implementations found for: %s (not proof none exist)\n", name)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "🔧 Implementations of %s (%d found):\n", name, len(records))
	for _, record := range records {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s [%s] %s:%d (%s; %s)\n", record.Name, record.Kind, record.File, record.Line, record.Origin, strings.Join(record.Evidence, ","))
	}
	return nil
}

func lookupImplementations(ctx context.Context, cmd *cobra.Command, cwd string, cfg *config.Config, manager *db.Manager, name string, languages []string) (records []implementationRecord, outcomes []db.PhaseOutcome, resultErr error) {
	records = []implementationRecord{}
	outcomes = []db.PhaseOutcome{}
	stored, err := manager.GetImplementationsByName(name, languages)
	if err != nil {
		return records, outcomes, err
	}
	relative := func(path string) string {
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return path
		}
		return rel
	}
	for _, symbol := range stored {
		records = append(records, implementationRecord{Name: symbol.Name, Kind: symbol.Kind, File: relative(symbol.File), Line: symbol.Line, ID: symbol.ID, Source: symbol.Source, Origin: "stored", Evidence: evidenceKinds(symbol.RelationshipEvidence)})
	}
	if len(records) > 0 {
		return records, outcomes, nil
	}
	symbols, err := manager.GetSymbolByName(name, languages)
	if err != nil {
		return records, outcomes, err
	}
	root := lsp.NewRoot(cwd)
	lines := lsp.NewSourceLines()
	providers := lsp.NewManager(cfg, root.URI())
	defer func() {
		if err := providers.ShutdownAll(); err != nil {
			warnLiveLSP(cmd, "cleanup", err)
			outcomes = append(outcomes, db.PhaseOutcome{Phase: "cleanup", Provider: "lsp", Status: "failed", Reason: err.Error()})
		}
	}()
	successes := 0
	var failures []error
	for _, symbol := range symbols {
		if err := ctx.Err(); err != nil {
			return records, outcomes, err
		}
		if symbol.Kind != "interface" && symbol.Kind != "class" && symbol.Kind != "struct" {
			continue
		}
		phase := db.PhaseOutcome{Language: symbol.Language, Phase: "implementations", Provider: "lsp", Status: "complete"}
		client, err := providers.GetClient(ctx, symbol.Language)
		var implementations []lsp.Location
		if err == nil {
			pos := lines.ToLSP(client.PositionEncoding(), symbol.File, symbol.Line, symbol.Column)
			implementations, err = client.Implementation(ctx, lsp.PathToURI(symbol.File), pos)
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return records, outcomes, ctxErr
			}
			phase.Status = "failed"
			if lsp.IsUnsupported(err) {
				phase.Status = "unsupported"
			}
			phase.Reason = err.Error()
			phase.FailedRequests = 1
			outcomes = append(outcomes, phase)
			failures = append(failures, err)
			warnLiveLSP(cmd, symbol.Language, err)
			continue
		}
		successes++
		for _, implementation := range implementations {
			path, pathErr := root.Path(implementation.URI)
			if pathErr != nil {
				path = implementation.URI
			}
			line, _ := lines.FromLSP(client.PositionEncoding(), path, implementation.Range.Start)
			records = append(records, implementationRecord{File: relative(path), Line: line, Source: "lsp", Origin: "live", Evidence: []string{"lsp-implementation"}})
		}
		phase.Records = len(implementations)
		outcomes = append(outcomes, phase)
	}
	if successes == 0 && len(failures) > 0 {
		return records, outcomes, fmt.Errorf("all live implementation providers failed: %w", errors.Join(failures...))
	}
	return records, outcomes, nil
}

func warnLiveLSP(cmd *cobra.Command, language string, err error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "warning: live %s implementation lookup failed: %v\n", language, err)
}
