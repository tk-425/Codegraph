package indexer

import (
	"context"
	"testing"
	"time"
)

// T40: a server position on a line with a non-BMP character is stored as
// the same UTF-8 byte column whichever encoding the server chose.
func TestLSPPositionsAreStoredAsByteColumns(t *testing.T) {
	for _, responses := range []string{"positions-utf16.json", "positions-utf8.json"} {
		t.Run(responses, func(t *testing.T) {
			p := newTransportProject(t, "positions/nonbmp.go", "nonbmp.go")
			useScriptedServer(t, p.cfg, "go", responses)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := p.index(ctx, t); err != nil {
				t.Fatal(err)
			}
			targets := symbolsNamed(t, p.database, "Target")
			if len(targets) != 1 || targets[0].Source != "lsp" {
				t.Fatalf("want one LSP symbol, got %+v", targets)
			}
			if got := targets[0]; got.Line != 3 || got.Column != 14 || got.EndColumn == nil || *got.EndColumn != 25 {
				t.Errorf("stored %d:%d end %v, want 3:14 end 25", got.Line, got.Column, got.EndColumn)
			}
		})
	}
}
