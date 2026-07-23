package config

import "testing"

func TestDefaultConfigUsesAutomaticTypeScriptServer(t *testing.T) {
	cfg := DefaultConfig()
	for _, language := range []string{"typescript", "typescriptreact"} {
		got := cfg.LSP[language]
		if got.Command != "typescript-language-server" {
			t.Fatalf("%s command = %q", language, got.Command)
		}
		if len(got.Args) != 1 || got.Args[0] != "--stdio" {
			t.Fatalf("%s args = %#v", language, got.Args)
		}
	}
}
