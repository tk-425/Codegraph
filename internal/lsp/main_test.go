package lsp

import (
	"os"
	"testing"
)

// lifecycleBuildDir holds the protocol peer built once for this test binary.
var lifecycleBuildDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "codegraph-lsp-peer-")
	if err != nil {
		panic(err)
	}
	lifecycleBuildDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
