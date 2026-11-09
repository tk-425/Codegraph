package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tk-425/Codegraph/internal/lsp"
)

// jsonSupportAnnotation marks commands whose --json output is defined.
const jsonSupportAnnotation = "codegraph.json"

var rootCmd = &cobra.Command{
	Use:   "codegraph",
	Short: "Code indexing and call graph analysis tool",
	Long:  "CodeGraph indexes your codebase using LSP servers and provides fast symbol search, call graph analysis, and code navigation.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if jsonOutputFlag && cmd.Annotations[jsonSupportAnnotation] == "" {
			return withCode("json_unsupported", fmt.Errorf("%s does not support --json; rerun without it", cmd.CommandPath()))
		}
		return nil
	},
}

// jsonHelpRejected is set when help was requested in JSON mode, which has no
// JSON form; Execute turns it into a failure after its envelope is written.
var jsonHelpRejected error

// Execute runs the CLI with os.Args. In JSON mode it is the single output
// boundary: a failed command that emitted nothing gets exactly one error
// envelope, and a failed stdout write is never retried.
func Execute(ctx context.Context) error {
	return execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	jsonMode := jsonRequested(args)
	out := &trackingWriter{w: stdout}
	rootCmd.SetArgs(args)
	rootCmd.SetErr(stderr)
	if jsonMode {
		// Only JSON mode routes output through the tracker: Cobra sends usage
		// to the out writer whenever one is set, which would move human-mode
		// usage text from stderr to stdout.
		rootCmd.SetOut(out)
		// Cobra's own error and usage text must not reach stdout.
		rootCmd.SilenceErrors = true
		rootCmd.SilenceUsage = true
	}
	jsonHelpRejected = nil

	cmd, err := rootCmd.ExecuteContextC(ctx)
	if err == nil {
		return jsonHelpRejected
	}
	if !jsonMode || out.wrote || out.err != nil {
		return err
	}
	name := rootCmd.Name()
	if cmd != nil {
		name = cmd.Name()
	}
	_ = EmitJSON(out, name, nil, []any{}, []EnvelopeError{{Code: errorCode(err), Message: err.Error()}})
	return err
}

// jsonRequested scans raw arguments, so a flag error anywhere on the command
// line still produces an envelope even if pflag stopped before --json.
func jsonRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" {
			return true
		}
		if value, ok := strings.CutPrefix(arg, "--json="); ok {
			enabled, err := strconv.ParseBool(value)
			return err != nil || enabled
		}
	}
	return false
}

// trackingWriter records whether stdout received any bytes and whether a
// write failed, so the boundary never emits a second or retried payload.
type trackingWriter struct {
	w     io.Writer
	wrote bool
	err   error
}

func (t *trackingWriter) Write(p []byte) (int, error) {
	t.wrote = true
	n, err := t.w.Write(p)
	if err != nil && t.err == nil {
		t.err = err
	}
	return n, err
}

// codedError carries a stable JSON error code to the output boundary.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

func withCode(code string, err error) error {
	return &codedError{code: code, err: err}
}

// failure codes err, preferring "cancelled" when the command was cancelled.
func failure(code string, err error) error {
	return withCode(failureCode(code, err), err)
}

func failureCode(code string, err error) string {
	if isCancellation(err) {
		return "cancelled"
	}
	return code
}

func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled)
}

func errorCode(err error) string {
	var coded *codedError
	if errors.As(err, &coded) {
		return coded.code
	}
	if isCancellation(err) {
		return "cancelled"
	}
	// Cobra reports an unknown subcommand as an untyped error.
	if strings.HasPrefix(err.Error(), "unknown command") {
		return "invalid_arguments"
	}
	return "command_failed"
}

// commandContext returns the command's context, or Background when a test
// invokes RunE without Execute.
func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// supportJSON marks a command's --json output as defined and codes its
// argument validation failures for the boundary.
func supportJSON(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[jsonSupportAnnotation] = "supported"
	if validate := cmd.Args; validate != nil {
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			if err := validate(cmd, args); err != nil {
				return withCode("invalid_arguments", err)
			}
			return nil
		}
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutputFlag, "json", false, "Emit one machine-readable JSON envelope (search, signature, callers, callees, implementations, types, health, stats, projects, build)")
	rootCmd.PersistentFlags().BoolVar(&lsp.StderrPassthrough, "lsp-stderr", false, "Forward raw language-server standard error instead of discarding it")
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return withCode("invalid_arguments", err)
	})

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		out := cmd.OutOrStdout()
		if jsonOutputFlag {
			jsonHelpRejected = withCode("json_unsupported", errors.New("help is not available as JSON; rerun without --json"))
			_ = EmitJSON(out, "help", nil, []any{}, []EnvelopeError{{Code: "json_unsupported", Message: jsonHelpRejected.Error()}})
			return
		}
		printBanner(out)
		fmt.Fprintln(out)
		defaultHelp(cmd, args)
	})
}
