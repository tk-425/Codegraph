package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
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
	rootCmd.AddCommand(searchCmd)
}

type searchRecord struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Language  string `json:"language"`
	Signature string `json:"signature"`
}

func runSearch(cmd *cobra.Command, args []string) error {
	symbol := args[0]
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return runSearchJSON(cmd, symbol)
	}

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if codegraph is initialized
	codegraphDir := filepath.Join(cwd, ".codegraph")
	if _, err := os.Stat(codegraphDir); os.IsNotExist(err) {
		return fmt.Errorf("codegraph not initialized. Run 'codegraph init' first")
	}

	// Load config
	cfg, err := config.Load(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Open database
	dbPath := cfg.GetDatabasePath(cwd)
	dbManager, err := db.NewManager(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer dbManager.Close()

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
	ctx := context.Background()
	results, err := orchestrator.Search(ctx, opts)
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
		fmt.Printf("  %s [%s]\n", Symbol(r.Name), Keyword(r.Kind))
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
	emitErr := func(code string, err error) error {
		_ = EmitJSON(out, "search", &symbol, []searchRecord{}, []EnvelopeError{{Code: code, Message: err.Error()}})
		return err
	}

	cwd, _, dbManager, code, err := openProject(false)
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

	results, err := orchestrator.Search(context.Background(), opts)
	if err != nil {
		return emitErr("search_failed", fmt.Errorf("search failed: %w", err))
	}

	records := make([]searchRecord, 0, len(results))
	for _, r := range results {
		relPath, rerr := filepath.Rel(cwd, r.File)
		if rerr != nil {
			relPath = r.File
		}
		records = append(records, searchRecord{
			Name:      r.Name,
			Kind:      r.Kind,
			File:      relPath,
			Line:      r.Line,
			Language:  r.Language,
			Signature: r.Signature,
		})
	}

	return EmitJSON(out, "search", &symbol, records, nil)
}
