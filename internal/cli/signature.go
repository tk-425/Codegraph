package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/db"
)

var signatureLangFlag string

var signatureCmd = &cobra.Command{
	Use:   "signature <symbol>",
	Short: "Get the signature of a function or method",
	Long: `Get the full signature of a function or method including parameters and return type.

Examples:
  codegraph signature parseConfig
  codegraph signature handleRequest --lang=go`,
	Args: cobra.ExactArgs(1),
	RunE: runSignature,
}

func init() {
	signatureCmd.Flags().StringVar(&signatureLangFlag, "lang", "", "Filter by language(s), comma-separated")
	supportJSON(signatureCmd)
	rootCmd.AddCommand(signatureCmd)
}

type signatureRecord struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Origin    string `json:"origin"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Language  string `json:"language"`
	Signature string `json:"signature"`
}

func runSignature(cmd *cobra.Command, args []string) error {
	symbol := args[0]
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return runSignatureJSON(cmd, symbol)
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
	if signatureLangFlag != "" {
		languages = strings.Split(signatureLangFlag, ",")
	}

	// Find symbols in database
	symbols, err := dbManager.GetSymbolByName(symbol, languages)
	if err != nil {
		return fmt.Errorf("failed to find symbol: %w", err)
	}

	// Filter to only functions/methods (and variables for OCaml where functions are let-bindings)
	var filtered []db.Symbol
	for _, sym := range symbols {
		if sym.Kind == "function" || sym.Kind == "method" || sym.Kind == "variable" {
			filtered = append(filtered, sym)
		}
	}

	if len(filtered) == 0 {
		fmt.Printf("📝 No function/method named '%s' found\n", Warning(symbol))
		return nil
	}

	fmt.Printf("📝 Signature for '%s' (%s found):\n\n", Symbol(symbol), Info(len(filtered)))

	for _, sym := range filtered {
		relPath, _ := filepath.Rel(cwd, sym.File)

		fmt.Printf("  %s [%s] (%s)\n", Symbol(sym.Name), Keyword(sym.Kind), sym.Source)
		fmt.Printf("    %s\n", Path(fmt.Sprintf("%s:%d", relPath, sym.Line)))

		// Show signature and source line
		sourceLine := getSourceLine(sym.File, sym.Line)
		if sym.Signature != "" && strings.TrimSpace(sym.Signature) != "" {
			// Show: signature → source
			sig := strings.TrimSpace(sym.Signature)
			if sourceLine != "" {
				fmt.Printf("    %s → %s\n", Type(sig), Dim(sourceLine))
			} else {
				fmt.Printf("    %s\n", Type(sig))
			}
		} else if sourceLine != "" {
			fmt.Printf("    %s\n", Dim(sourceLine))
		}
		fmt.Println()
	}

	return nil
}

func runSignatureJSON(cmd *cobra.Command, symbol string) error {
	out := cmd.OutOrStdout()
	emitErr := func(code string, err error) error {
		_ = EmitJSON(out, "signature", &symbol, []signatureRecord{}, []EnvelopeError{{Code: code, Message: err.Error()}})
		return err
	}

	cwd, _, dbManager, code, err := openProject(commandContext(cmd))
	if err != nil {
		return emitErr(code, err)
	}
	defer dbManager.Close()

	var languages []string
	if signatureLangFlag != "" {
		languages = strings.Split(signatureLangFlag, ",")
	}

	symbols, err := dbManager.GetSymbolByName(symbol, languages)
	if err != nil {
		return emitErr("signature_lookup_failed", fmt.Errorf("failed to find symbol: %w", err))
	}

	records := make([]signatureRecord, 0, len(symbols))
	for _, sym := range symbols {
		if sym.Kind != "function" && sym.Kind != "method" && sym.Kind != "variable" {
			continue
		}
		relPath, rerr := filepath.Rel(cwd, sym.File)
		if rerr != nil {
			relPath = sym.File
		}
		records = append(records, signatureRecord{
			ID: sym.ID, Source: sym.Source, Origin: "stored",
			Name:      sym.Name,
			Kind:      sym.Kind,
			File:      relPath,
			Line:      sym.Line,
			Language:  sym.Language,
			Signature: strings.TrimSpace(sym.Signature),
		})
	}

	metadata, err := queryMetadata(dbManager)
	if err != nil {
		return emitErr("signature_lookup_failed", err)
	}
	return EmitJSON(out, "signature", &symbol, records, nil, metadata)
}

// colorizeSignature adds colors to a function signature
func colorizeSignature(sig string) string {
	// Simple colorization: func keyword in cyan
	if len(sig) >= 4 && sig[:4] == "func" {
		return Keyword("func") + sig[4:]
	}
	return sig
}
