package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	callersDepthFlag int
	callersLangFlag  string
)

var callersCmd = &cobra.Command{
	Use:   "callers <symbol>",
	Short: "Find all functions that call a given symbol",
	Long: `Find all functions that call the specified symbol.

Examples:
  codegraph callers parseConfig
  codegraph callers handleRequest --depth=2
  codegraph callers parse --lang=go,python`,
	Args: cobra.ExactArgs(1),
	RunE: runCallers,
}

func init() {
	callersCmd.Flags().IntVar(&callersDepthFlag, "depth", 1, "Depth of call chain to traverse")
	callersCmd.Flags().StringVar(&callersLangFlag, "lang", "", "Filter by language(s), comma-separated")
	supportJSON(callersCmd)
	rootCmd.AddCommand(callersCmd)
}

type callerRecord struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Origin   string   `json:"origin"`
	Evidence []string `json:"evidence"`
	Column   int      `json:"column"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
}

func runCallers(cmd *cobra.Command, args []string) error {
	if callersDepthFlag != 1 {
		return withCode("not_implemented", fmt.Errorf("--depth=%d is not supported; only --depth=1 (direct callers) is implemented", callersDepthFlag))
	}
	symbol := args[0]
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return runCallersJSON(cmd, symbol)
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
	if callersLangFlag != "" {
		languages = strings.Split(callersLangFlag, ",")
	}

	// Find callers
	callers, err := dbManager.GetCallers(symbol, languages)
	if err != nil {
		return fmt.Errorf("failed to find callers: %w", err)
	}

	if len(callers) == 0 {
		fmt.Printf("📞 No callers found for: %s\n", Warning(symbol))
		return nil
	}

	fmt.Printf("📞 Callers of %s (%s found):\n\n", Symbol(symbol), Info(len(callers)))
	for _, c := range callers {
		relPath, _ := filepath.Rel(cwd, c.CallFile)
		fmt.Printf("  %s [%s] (%s; %s)\n", Symbol(c.Name), Keyword(c.Kind), c.Source, c.Evidence)
		fmt.Printf("    %s\n", Path(fmt.Sprintf("%s:%d", relPath, c.CallLine)))

		// Show the actual source line
		if line := getSourceLine(c.CallFile, c.CallLine); line != "" {
			fmt.Printf("    %s\n", Dim(line))
		}
		fmt.Println()
	}

	return nil
}

func runCallersJSON(cmd *cobra.Command, symbol string) error {
	out := cmd.OutOrStdout()
	emitErr := func(code string, err error) error {
		_ = EmitJSON(out, "callers", &symbol, []callerRecord{}, []EnvelopeError{{Code: code, Message: err.Error()}})
		return err
	}

	cwd, _, dbManager, code, err := openProject(commandContext(cmd))
	if err != nil {
		return emitErr(code, err)
	}
	defer dbManager.Close()

	var languages []string
	if callersLangFlag != "" {
		languages = strings.Split(callersLangFlag, ",")
	}

	callers, err := dbManager.GetCallers(symbol, languages)
	if err != nil {
		return emitErr("callers_lookup_failed", fmt.Errorf("failed to find callers: %w", err))
	}

	records := make([]callerRecord, 0, len(callers))
	for _, c := range callers {
		relPath, rerr := filepath.Rel(cwd, c.CallFile)
		if rerr != nil {
			relPath = c.CallFile
		}
		records = append(records, callerRecord{
			ID: c.ID, Source: c.Source, Origin: "stored", Evidence: evidenceKinds(c.Evidence), Column: c.CallColumn,
			Name: c.Name,
			Kind: c.Kind,
			File: relPath,
			Line: c.CallLine,
		})
	}

	metadata, err := queryMetadata(dbManager)
	if err != nil {
		return emitErr("callers_lookup_failed", err)
	}
	return EmitJSON(out, "callers", &symbol, records, nil, metadata)
}

// getSourceLine reads a specific line from a file
func getSourceLine(filePath string, lineNum int) string {
	file, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	currentLine := 0
	for scanner.Scan() {
		currentLine++
		if currentLine == lineNum {
			return strings.TrimSpace(scanner.Text())
		}
	}
	return ""
}
