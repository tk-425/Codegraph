package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/search"
)

var (
	searchKindFlag  string
	searchLangFlag  string
	searchLimitFlag int
	searchExactFlag bool
)

var searchCmd = &cobra.Command{
	Use:   "search <symbol>",
	Short: "Search for symbols by name",
	Long: `Search for symbols (functions, variables, classes, etc.) by name.

Uses multi-tier search: database first, then ripgrep fallback.

Examples:
  codegraph search parseConfig
  codegraph search parse --kind=function
  codegraph search Config --lang=go,python
  codegraph search main --exact`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().StringVar(&searchKindFlag, "kind", "", "Filter by symbol kind (function, variable, class, interface, type, module)")
	searchCmd.Flags().StringVar(&searchLangFlag, "lang", "", "Filter by language(s), comma-separated (e.g., go,python)")
	searchCmd.Flags().IntVar(&searchLimitFlag, "limit", 20, "Max results to show")
	searchCmd.Flags().BoolVar(&searchExactFlag, "exact", false, "Require exact name match")
	supportJSON(searchCmd)
	rootCmd.AddCommand(searchCmd)
}

type searchRecord struct {
	ID        string `json:"id,omitempty"`
	Source    string `json:"source"`
	Origin    string `json:"origin"`
	Context   string `json:"context,omitempty"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Language  string `json:"language"`
	Signature string `json:"signature"`
}

func runSearch(cmd *cobra.Command, args []string) error {
	if searchLimitFlag < 0 {
		return withCode("invalid_arguments", fmt.Errorf("--limit must be nonnegative (0 means unlimited)"))
	}
	symbol := args[0]
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return runSearchJSON(cmd, symbol)
	}

	cwd, _, dbManager, _, err := openProject(commandContext(cmd))
	if err != nil {
		return err
	}
	defer dbManager.Close()
	if err := printQueryMetadata(cmd, dbManager); err != nil {
		return err
	}

	// Parse languages filter
	var languages []string
	if searchLangFlag != "" {
		languages = strings.Split(searchLangFlag, ",")
	}

	// Create search tiers
	dbTier := search.NewDatabaseTier(dbManager)
	rgTier := search.NewRipgrepTier(cwd)

	// Create orchestrator with fallback chain
	orchestrator := search.NewOrchestrator(dbTier, rgTier)

	// Search options
	opts := search.SearchOptions{
		Query:      symbol,
		Kind:       searchKindFlag,
		Languages:  languages,
		Limit:      searchLimitFlag,
		ExactMatch: searchExactFlag,
	}

	// Execute search
	results, report, err := orchestrator.Search(commandContext(cmd), opts)
	warnSearchReport(cmd.ErrOrStderr(), report)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		fmt.Printf("🔍 No results found for: %s\n", Warning(symbol))
		return nil
	}

	fmt.Printf("🔍 Found %s results for '%s':\n\n", Info(len(results)), Symbol(symbol))
	for _, r := range results {
		relPath, err := filepath.Rel(cwd, r.File)
		if err != nil {
			relPath = r.File
		}
		fmt.Printf("  %s [%s] (%s)\n", Symbol(r.Name), Keyword(r.Kind), r.Source)
		fmt.Printf("    %s\n", Path(fmt.Sprintf("%s:%d", relPath, r.Line)))

		// Show signature if available, otherwise show source line
		if r.Signature != "" {
			fmt.Printf("    %s\n", colorizeSignature(r.Signature))
		} else if r.Context != "" {
			// Show context for ripgrep matches
			fmt.Printf("    %s\n", Dim(r.Context))
		} else if line := getSourceLine(r.File, r.Line); line != "" {
			fmt.Printf("    %s\n", Dim(line))
		}
		fmt.Println()
	}

	return nil
}

func runSearchJSON(cmd *cobra.Command, symbol string) error {
	out := cmd.OutOrStdout()
	emitErr := func(code string, err error, metadata ...*OutputMetadata) error {
		_ = EmitJSON(out, "search", &symbol, []searchRecord{}, []EnvelopeError{{Code: code, Message: err.Error()}}, metadata...)
		return err
	}

	cwd, _, dbManager, code, err := openProject(commandContext(cmd))
	if err != nil {
		return emitErr(code, err)
	}
	defer dbManager.Close()

	var languages []string
	if searchLangFlag != "" {
		languages = strings.Split(searchLangFlag, ",")
	}

	dbTier := search.NewDatabaseTier(dbManager)
	rgTier := search.NewRipgrepTier(cwd)
	orchestrator := search.NewOrchestrator(dbTier, rgTier)

	opts := search.SearchOptions{
		Query:      symbol,
		Kind:       searchKindFlag,
		Languages:  languages,
		Limit:      searchLimitFlag,
		ExactMatch: searchExactFlag,
	}

	results, report, err := orchestrator.Search(commandContext(cmd), opts)
	warnSearchReport(cmd.ErrOrStderr(), report)
	if err != nil {
		return emitErr(failureCode("search_failed", err), fmt.Errorf("search failed: %w", err), &OutputMetadata{Freshness: "unchecked", Search: &report})
	}

	records := make([]searchRecord, 0, len(results))
	for _, r := range results {
		relPath, rerr := filepath.Rel(cwd, r.File)
		if rerr != nil {
			relPath = r.File
		}
		origin := "stored"
		if r.Source == "ripgrep" {
			origin = "text"
		}
		records = append(records, searchRecord{
			ID: r.ID, Source: r.Source, Origin: origin, Context: r.Context,
			Name:      r.Name,
			Kind:      r.Kind,
			File:      relPath,
			Line:      r.Line,
			Language:  r.Language,
			Signature: r.Signature,
		})
	}

	metadata, err := queryMetadata(dbManager)
	if err != nil {
		return emitErr("search_failed", err)
	}
	if len(report.Skipped) > 0 {
		metadata.Search = &report
	}
	return EmitJSON(out, "search", &symbol, records, nil, metadata)
}

// warnSearchReport keeps skipped fallbacks and provider errors off stdout.
func warnSearchReport(w io.Writer, report search.SearchReport) {
	for _, failure := range report.Failures {
		fmt.Fprintf(w, "warning: %s search tier failed: %v\n", failure.Tier, failure.Err)
	}
	for _, skip := range report.Skipped {
		fmt.Fprintf(w, "%s search tier skipped: %s\n", skip.Tier, skip.Reason)
	}
}
