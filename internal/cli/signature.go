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
	Short: "Show stored signature information and documentation",
	Long: `Show stored signature information and documentation for functions, methods and variables.

Availability means stored content exists, not that a signature is complete. LSP detail
may be partial; syntax extraction preserves declaration headers without inferring types.
Missing signature information or documentation is marked unavailable. This read-only
query never substitutes live source text or rebuilds the index.

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
	ID                  string `json:"id"`
	Source              string `json:"source"`
	Origin              string `json:"origin"`
	Name                string `json:"name"`
	Kind                string `json:"kind"`
	File                string `json:"file"`
	Line                int    `json:"line"`
	Language            string `json:"language"`
	Signature           string `json:"signature"`
	Documentation       string `json:"documentation"`
	SignatureStatus     string `json:"signature_status"`
	DocumentationStatus string `json:"documentation_status"`
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

	records := signatureRecords(cwd, symbols)
	if len(records) == 0 {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "📝 No function/method/variable named '%s' found\n", Warning(symbol))
		return err
	}

	var text strings.Builder
	fmt.Fprintf(&text, "📝 Stored signature information for '%s' (%s found; completeness not guaranteed):\n\n", Symbol(symbol), Info(len(records)))
	for _, record := range records {
		fmt.Fprintf(&text, "  %s [%s] (%s)\n", Symbol(record.Name), Keyword(record.Kind), record.Source)
		fmt.Fprintf(&text, "    %s\n", Path(fmt.Sprintf("%s:%d", record.File, record.Line)))
		if record.SignatureStatus == "available" {
			fmt.Fprintf(&text, "    Signature information: %s\n", Type(record.Signature))
		} else {
			fmt.Fprintln(&text, "    Signature information: unavailable")
		}
		if record.DocumentationStatus == "available" {
			fmt.Fprintf(&text, "    Documentation: %s\n", record.Documentation)
		} else {
			fmt.Fprintln(&text, "    Documentation: unavailable")
		}
		fmt.Fprintln(&text)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), text.String())
	return err
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

	records := signatureRecords(cwd, symbols)

	metadata, err := queryMetadata(dbManager)
	if err != nil {
		return emitErr("signature_lookup_failed", err)
	}
	return EmitJSON(out, "signature", &symbol, records, nil, metadata)
}

// signatureRecords keeps filtering and availability identical in both output modes.
func signatureRecords(cwd string, symbols []db.Symbol) []signatureRecord {
	records := make([]signatureRecord, 0, len(symbols))
	for _, sym := range symbols {
		// OCaml functions can be stored as variable let-bindings.
		if sym.Kind != "function" && sym.Kind != "method" && sym.Kind != "variable" {
			continue
		}
		relPath, err := filepath.Rel(cwd, sym.File)
		if err != nil {
			relPath = sym.File
		}
		record := signatureRecord{
			ID: sym.ID, Source: sym.Source, Origin: "stored",
			Name: sym.Name, Kind: sym.Kind, File: relPath, Line: sym.Line, Language: sym.Language,
			Signature: strings.TrimSpace(sym.Signature), Documentation: strings.TrimSpace(sym.Documentation),
			SignatureStatus: "unavailable", DocumentationStatus: "unavailable",
		}
		if record.Signature != "" {
			record.SignatureStatus = "available"
		}
		if record.Documentation != "" {
			record.DocumentationStatus = "available"
		}
		records = append(records, record)
	}
	return records
}

// colorizeSignature adds colors to a function signature
func colorizeSignature(sig string) string {
	// Simple colorization: func keyword in cyan
	if len(sig) >= 4 && sig[:4] == "func" {
		return Keyword("func") + sig[4:]
	}
	return sig
}
