package lsp

import (
	"os"
	"strings"
)

// PositionEncoding is what an LSP character offset counts (LSP 3.17
// PositionEncodingKind). CodeGraph stores columns as UTF-8 byte offsets.
type PositionEncoding string

const (
	EncodingUTF8  PositionEncoding = "utf-8"
	EncodingUTF16 PositionEncoding = "utf-16"
	EncodingUTF32 PositionEncoding = "utf-32"
)

// negotiatedEncoding is the server's choice, defaulting to UTF-16 when it
// names none or one the client did not offer.
func negotiatedEncoding(chosen string) PositionEncoding {
	switch e := PositionEncoding(chosen); e {
	case EncodingUTF8, EncodingUTF32:
		return e
	default:
		return EncodingUTF16
	}
}

// ByteColumn converts a character offset in line to a UTF-8 byte offset.
// Offsets past the end clamp to the line length.
func (e PositionEncoding) ByteColumn(line string, character int) int {
	if e == EncodingUTF8 {
		return min(max(character, 0), len(line))
	}
	units := 0
	for i, r := range line {
		if units >= character {
			return i
		}
		units += e.width(r)
	}
	return len(line)
}

// Character converts a UTF-8 byte offset in line to a character offset.
func (e PositionEncoding) Character(line string, byteColumn int) int {
	byteColumn = min(max(byteColumn, 0), len(line))
	if e == EncodingUTF8 {
		return byteColumn
	}
	units := 0
	for _, r := range line[:byteColumn] {
		units += e.width(r)
	}
	return units
}

// width is the code units r occupies; an invalid byte counts as one.
func (e PositionEncoding) width(r rune) int {
	if e == EncodingUTF16 && r > 0xFFFF {
		return 2
	}
	return 1
}

// SourceLines converts between stored positions (1-based line, UTF-8 byte
// column) and LSP positions, reading each file at most once.
type SourceLines struct {
	files map[string][]string
}

func NewSourceLines() *SourceLines {
	return &SourceLines{files: map[string][]string{}}
}

// Set records content already read for path.
func (s *SourceLines) Set(path, content string) {
	s.files[path] = splitLines(content)
}

// Line returns 1-based line of path, or "" when unavailable.
func (s *SourceLines) Line(path string, line int) string {
	lines, ok := s.files[path]
	if !ok {
		content, err := os.ReadFile(path)
		if err == nil {
			lines = splitLines(string(content))
		}
		s.files[path] = lines
	}
	if line < 1 || line > len(lines) {
		return ""
	}
	return lines[line-1]
}

// ToLSP converts a stored position to an LSP position in encoding e.
func (s *SourceLines) ToLSP(e PositionEncoding, path string, line, byteColumn int) Position {
	return Position{Line: line - 1, Character: e.Character(s.Line(path, line), byteColumn)}
}

// FromLSP converts an LSP position in encoding e to a stored position.
func (s *SourceLines) FromLSP(e PositionEncoding, path string, pos Position) (line, byteColumn int) {
	line = pos.Line + 1
	return line, e.ByteColumn(s.Line(path, line), pos.Character)
}

func splitLines(content string) []string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}
