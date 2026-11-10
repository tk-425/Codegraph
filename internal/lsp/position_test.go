package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// "a𐐀b" is the LSP 3.17 example: b is UTF-16 offset 3, UTF-32 offset 2 and
// UTF-8 offset 5.
func TestPositionEncodingsConvertNonBMPText(t *testing.T) {
	line := "a𐐀b"
	for _, test := range []struct {
		enc       PositionEncoding
		character int
	}{
		{EncodingUTF16, 3},
		{EncodingUTF32, 2},
		{EncodingUTF8, 5},
	} {
		if got := test.enc.ByteColumn(line, test.character); got != 5 {
			t.Errorf("%s ByteColumn(%d) = %d, want 5", test.enc, test.character, got)
		}
		if got := test.enc.Character(line, 5); got != test.character {
			t.Errorf("%s Character(5) = %d, want %d", test.enc, got, test.character)
		}
	}
	if got := EncodingUTF16.ByteColumn("é", 99); got != len("é") {
		t.Errorf("past-end offset = %d, want clamp to %d", got, len("é"))
	}
}

func TestNegotiatedEncodingDefaultsToUTF16(t *testing.T) {
	for chosen, want := range map[string]PositionEncoding{"": EncodingUTF16, "utf-16": EncodingUTF16, "utf-8": EncodingUTF8, "utf-32": EncodingUTF32, "latin-1": EncodingUTF16} {
		if got := negotiatedEncoding(chosen); got != want {
			t.Errorf("negotiatedEncoding(%q) = %s, want %s", chosen, got, want)
		}
	}
	if offered := DefaultClientCapabilities().General.PositionEncodings; len(offered) != 2 || offered[0] != EncodingUTF8 || offered[1] != EncodingUTF16 {
		t.Errorf("client offers %v, want [utf-8 utf-16]", offered)
	}
}

func TestSourceLinesRoundTripStoredPositions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("x\r\n/*𐐀*/ func Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := NewSourceLines()
	pos := lines.ToLSP(EncodingUTF16, path, 2, 14)
	if pos.Line != 1 || pos.Character != 12 {
		t.Fatalf("ToLSP = %+v, want line 1 character 12", pos)
	}
	if line, column := lines.FromLSP(EncodingUTF16, path, pos); line != 2 || column != 14 {
		t.Errorf("FromLSP = %d:%d, want 2:14", line, column)
	}
}
