package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/indexer"
	"github.com/tk-425/Codegraph/internal/lsp"
)

var forceFlag bool

type buildDiagnosticRecord struct {
	Language         string `json:"language"`
	Executable       string `json:"executable"`
	Category         string `json:"category"`
	Severity         string `json:"severity"`
	Reason           string `json:"reason"`
	Command          string `json:"command,omitempty"`
	DocumentationURL string `json:"documentation_url,omitempty"`
	ExplicitOverride bool   `json:"explicit_override"`
}

func buildDiagnosticRecords(diagnostics []lsp.LSPDiagnostic) []buildDiagnosticRecord {
	records := make([]buildDiagnosticRecord, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		record := buildDiagnosticRecord{Language: diagnostic.Language, Executable: diagnostic.Executable, Category: string(diagnostic.Category), Severity: string(diagnostic.Severity), Reason: diagnostic.Reason, ExplicitOverride: diagnostic.Overridden}
		if diagnostic.Guidance != nil {
			record.Command = diagnostic.Guidance.Command
			record.DocumentationURL = diagnostic.Guidance.Documentation
		}
		records = append(records, record)
	}
	return records
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build or rebuild the symbol database",
	Long: "Build the codegraph database by indexing all source files.\n\n" +
		"This command:\n" +
		"1. Scans for source files (respecting .codegraph/.cgignore)\n" +
		"2. Starts LSP servers for detected languages\n" +
		"3. Extracts symbols from all source files\n" +
		"4. Stores symbols in the database\n\n" +
		"Edit `.codegraph/.cgignore` and rerun `codegraph build` to change what gets indexed.\n\n" +
		"Use --force to perform a full rebuild (delete and recreate database).",
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().BoolVar(&forceFlag, "force", false, "Force full rebuild (delete and recreate database)")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	if !jsonOutputFlag {
		printBanner(cmd.OutOrStdout())
		fmt.Println()
	}

	if forceFlag && !jsonOutputFlag {
		fmt.Printf("🔄 %s\n", Bold("Force rebuilding database..."))
	} else if !jsonOutputFlag {
		fmt.Printf("🔨 %s\n", Bold("Building database..."))
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

	// Scan for files
	cgignorePath := filepath.Join(codegraphDir, ".cgignore")
	scanner, err := indexer.NewScanner(cwd, cgignorePath)
	if err != nil {
		return fmt.Errorf("failed to prepare scanner: %w", err)
	}
	files, err := scanner.Scan()
	if err != nil {
		return fmt.Errorf("failed to scan files: %w", err)
	}

	languages := indexer.DetectedLanguages(files)
	if len(languages) == 0 {
		if jsonOutputFlag {
			return EmitJSON(cmd.OutOrStdout(), "build", nil, []buildDiagnosticRecord{}, nil)
		}
		fmt.Printf("⚠️  %s\n", Warning("No supported source files found"))
		return nil
	}
	if !jsonOutputFlag {
		fmt.Printf("🔍 Found %s files in %s languages (%s)\n", Info(len(files)), Info(len(languages)), Keyword(strings.Join(languages, ", ")))
	}

	// Open database
	dbPath := cfg.GetDatabasePath(cwd)
	dbManager, err := db.NewManager(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer dbManager.Close()

	if err := dbManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Create indexer and run
	idx := indexer.NewIndexer(cfg, dbManager, cwd)
	if jsonOutputFlag {
		idx.SetProgressWriter(io.Discard)
	}
	defer idx.Close()

	ctx := context.Background()
	if err := idx.IndexProject(ctx, files, forceFlag); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	diagnostics := idx.Diagnostics()
	if jsonOutputFlag {
		return EmitJSON(cmd.OutOrStdout(), "build", nil, buildDiagnosticRecords(diagnostics), nil)
	}
	printDiagnostics(os.Stdout, diagnostics)
	return nil
}

// printDiagnostics renders diagnostics for the human-readable build path. A
// server-log record describes a running server reporting a problem and is never
// rendered as unavailability.
func printDiagnostics(out io.Writer, diagnostics []lsp.LSPDiagnostic) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Category == lsp.ServerLog {
			fmt.Fprintf(out, "⚠️  %s LSP server reported a problem (%s) [%s]: %s\n", diagnostic.Language, diagnostic.Executable, diagnostic.Severity, diagnostic.Reason)
			continue
		}
		fmt.Fprintf(out, "⚠️  %s LSP unavailable (%s): %s\n", diagnostic.Language, diagnostic.Executable, diagnostic.Reason)
		if diagnostic.Guidance == nil {
			continue
		}
		if diagnostic.Guidance.Command != "" {
			fmt.Fprintf(out, "   Installation guidance: %s\n", diagnostic.Guidance.Command)
		}
		if diagnostic.Guidance.Documentation != "" {
			fmt.Fprintf(out, "   Documentation: %s\n", diagnostic.Guidance.Documentation)
		}
	}
}
