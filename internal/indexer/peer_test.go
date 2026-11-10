package indexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

var (
	peerDir   string
	peerOnce  sync.Once
	peerPath  string
	peerBuild error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if peerDir != "" {
		os.RemoveAll(peerDir)
	}
	os.Exit(code)
}

// scriptedPeer builds the protocol peer once per test binary.
func scriptedPeer(t *testing.T) string {
	t.Helper()
	peerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "codegraph-peer-")
		if err != nil {
			peerBuild = err
			return
		}
		peerDir = dir
		peerPath = filepath.Join(dir, "scripted-lsp")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "go", "build", "-o", peerPath, peerFixture("")).CombinedOutput(); err != nil {
			peerBuild = fmt.Errorf("build protocol peer: %v\n%s", err, output)
		}
	})
	if peerBuild != nil {
		t.Fatal(peerBuild)
	}
	return peerPath
}

func peerFixture(name string) string {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "characterization", "lsp"))
	if err != nil {
		panic(err)
	}
	return filepath.Join(root, name)
}

// useScriptedServer makes language answer from a responses fixture.
func useScriptedServer(t *testing.T, cfg *config.Config, language, responses string) {
	t.Helper()
	cfg.LSP[language] = config.LSPConfig{Command: scriptedPeer(t), Args: []string{"-responses", peerFixture(responses)}}
}
