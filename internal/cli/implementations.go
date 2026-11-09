package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/lsp"
)

var implementationsLangFlag string

var implementationsCmd = &cobra.Command{
	Use:   "implementations <interface>",
	Short: "Find implementations of an interface",
	Long: `Find all types that implement the specified interface.

Examples:
  codegraph implementations Reader
  codegraph implementations Service --lang=go`,
	Args: cobra.ExactArgs(1),
	RunE: runImplementations,
}

func init() {
	implementationsCmd.Flags().StringVar(&implementationsLangFlag, "lang", "", "Filter by language(s), comma-separated")
	supportJSON(implementationsCmd)
	rootCmd.AddCommand(implementationsCmd)
}

type implementationRecord struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

func runImplementations(cmd *cobra.Command, args []string) error {
	interfaceName := args[0]
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return runImplementationsJSON(cmd, interfaceName)
	}

	cwd, cfg, dbManager, _, err := openProject()
	if err != nil {
		return err
	}
	defer dbManager.Close()

	// First, try to find implementations in the database (from type_hierarchy table)
	dbImplementations, err := dbManager.GetImplementationsByName(interfaceName)
	if err != nil {
		return fmt.Errorf("failed to find implementations: %w", err)
	}
	if len(dbImplementations) > 0 {
		fmt.Printf("🔧 Implementations of %s (%s found):\n\n", Symbol(interfaceName), Info(len(dbImplementations)))
		for _, impl := range dbImplementations {
			relPath, _ := filepath.Rel(cwd, impl.File)
			fmt.Printf("  %s [%s]\n", Symbol(impl.Name), Keyword(impl.Kind))
			fmt.Printf("    %s\n", Path(fmt.Sprintf("%s:%d", relPath, impl.Line)))
			if line := getSourceLine(impl.File, impl.Line); line != "" {
				fmt.Printf("    %s\n", Dim(line))
			}
			fmt.Println()
		}
		return nil
	}

	// If no database results, try LSP as fallback
	// Parse languages filter
	var languages []string
	if implementationsLangFlag != "" {
		languages = strings.Split(implementationsLangFlag, ",")
	}

	// Find interface symbols in database
	symbols, err := dbManager.GetSymbolByName(interfaceName, languages)
	if err != nil {
		return fmt.Errorf("failed to find symbol: %w", err)
	}

	if len(symbols) == 0 {
		fmt.Printf("🔧 No interface named '%s' found in database\n", interfaceName)
		return nil
	}

	// Create LSP manager
	rootURI := "file://" + cwd
	lspManager := lsp.NewManager(cfg, rootURI)
	defer shutdownLiveLSP(cmd, lspManager)

	ctx := commandContext(cmd)
	found := false

	for _, sym := range symbols {
		// Only process interface-like symbols
		if sym.Kind != "interface" && sym.Kind != "class" && sym.Kind != "struct" {
			continue
		}

		// Get LSP client for this language
		client, err := lspManager.GetClient(ctx, sym.Language)
		if err != nil {
			continue
		}

		// Get implementations via LSP
		fileURI := "file://" + sym.File
		pos := lsp.Position{
			Line:      sym.Line - 1,
			Character: sym.Column,
		}

		implementations, err := client.Implementation(ctx, fileURI, pos)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			warnLiveLSP(cmd, sym.Language, err)
			continue
		}

		if len(implementations) > 0 {
			if !found {
				fmt.Printf("🔧 Implementations of %s (%s found via LSP):\n\n", Symbol(interfaceName), Info(len(implementations)))
				found = true
			}

			for _, impl := range implementations {
				implPath := strings.TrimPrefix(impl.URI, "file://")

				relPath, _ := filepath.Rel(cwd, implPath)
				fmt.Printf("  %s\n", Path(fmt.Sprintf("%s:%d", relPath, impl.Range.Start.Line+1)))
			}
		}
	}

	if !found {
		fmt.Printf("🔧 No implementations found for: %s\n", Warning(interfaceName))
	}

	return nil
}

func runImplementationsJSON(cmd *cobra.Command, interfaceName string) error {
	out := cmd.OutOrStdout()
	emitErr := func(code string, err error) error {
		_ = EmitJSON(out, "implementations", &interfaceName, []implementationRecord{}, []EnvelopeError{{Code: code, Message: err.Error()}})
		return err
	}

	cwd, cfg, dbManager, code, err := openProject()
	if err != nil {
		return emitErr(code, err)
	}
	defer dbManager.Close()

	records := make([]implementationRecord, 0)

	dbImpls, err := dbManager.GetImplementationsByName(interfaceName)
	if err != nil {
		return emitErr("implementations_lookup_failed", fmt.Errorf("failed to find implementations: %w", err))
	}
	for _, impl := range dbImpls {
		relPath, rerr := filepath.Rel(cwd, impl.File)
		if rerr != nil {
			relPath = impl.File
		}
		records = append(records, implementationRecord{
			Name: impl.Name,
			Kind: impl.Kind,
			File: relPath,
			Line: impl.Line,
		})
	}

	if len(records) > 0 {
		return EmitJSON(out, "implementations", &interfaceName, records, nil)
	}

	// LSP fallback
	var languages []string
	if implementationsLangFlag != "" {
		languages = strings.Split(implementationsLangFlag, ",")
	}

	symbols, err := dbManager.GetSymbolByName(interfaceName, languages)
	if err != nil {
		return emitErr("implementations_lookup_failed", fmt.Errorf("failed to find symbol: %w", err))
	}
	if len(symbols) == 0 {
		return EmitJSON(out, "implementations", &interfaceName, records, nil)
	}

	rootURI := "file://" + cwd
	lspManager := lsp.NewManager(cfg, rootURI)
	defer shutdownLiveLSP(cmd, lspManager)

	ctx := commandContext(cmd)
	for _, sym := range symbols {
		if sym.Kind != "interface" && sym.Kind != "class" && sym.Kind != "struct" {
			continue
		}
		client, cerr := lspManager.GetClient(ctx, sym.Language)
		if cerr != nil {
			continue
		}
		fileURI := "file://" + sym.File
		pos := lsp.Position{Line: sym.Line - 1, Character: sym.Column}
		impls, ierr := client.Implementation(ctx, fileURI, pos)
		if ierr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return emitErr("cancelled", ctxErr)
			}
			warnLiveLSP(cmd, sym.Language, ierr)
			continue
		}
		for _, impl := range impls {
			implPath := strings.TrimPrefix(impl.URI, "file://")
			relPath, rerr := filepath.Rel(cwd, implPath)
			if rerr != nil {
				relPath = implPath
			}
			records = append(records, implementationRecord{
				Name: "",
				Kind: "",
				File: relPath,
				Line: impl.Range.Start.Line + 1,
			})
		}
	}

	return EmitJSON(out, "implementations", &interfaceName, records, nil)
}

// warnLiveLSP reports a failed live lookup on stderr; stdout stays reserved
// for the command's own output.
func warnLiveLSP(cmd *cobra.Command, language string, err error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "warning: live %s implementation lookup failed: %v\n", language, err)
}

func shutdownLiveLSP(cmd *cobra.Command, manager *lsp.Manager) {
	if err := manager.ShutdownAll(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: LSP cleanup: %v\n", err)
	}
}
