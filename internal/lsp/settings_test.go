package lsp

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tk-425/Codegraph/internal/config"
)

// startSettingsPeer starts the scripted peer with -dump-init and returns the
// client plus the file the peer appends initialize params to.
func startSettingsPeer(t *testing.T) (*Client, string) {
	t.Helper()
	dump := filepath.Join(t.TempDir(), "init-params.json")
	rootURI := (&url.URL{Scheme: "file", Path: t.TempDir()}).String()
	client, err := NewClient(lifecyclePeer(t), []string{"-mode", "normal", "-dump-init", dump}, rootURI, "go")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil && !strings.Contains(err.Error(), "was killed") {
			t.Errorf("close: %v", err)
		}
	})
	return client, dump
}

// readDumpedInitialize decodes the one initialize params line the peer dumped.
func readDumpedInitialize(t *testing.T, path string) (InitializeParams, map[string]json.RawMessage) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("peer did not dump initialize params: %v", err)
	}
	line := strings.TrimSpace(string(data))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		t.Fatalf("dumped params are not JSON: %v\n%s", err, line)
	}
	var params InitializeParams
	if err := json.Unmarshal([]byte(line), &params); err != nil {
		t.Fatalf("dumped params do not decode: %v\n%s", err, line)
	}
	return params, raw
}

func TestInitializeDeliversServerSettings(t *testing.T) {
	client, dump := startSettingsPeer(t)
	client.SetServerSettings(map[string]any{"buildFlags": []string{"-tags=characterization,integration"}})
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	params, _ := readDumpedInitialize(t, dump)
	if params.InitializationOptions == nil {
		t.Fatalf("initializationOptions absent: %+v", params)
	}
	flags, ok := params.InitializationOptions["buildFlags"].([]any)
	if !ok || len(flags) != 1 || flags[0] != "-tags=characterization,integration" {
		t.Fatalf("buildFlags = %v", params.InitializationOptions["buildFlags"])
	}
	if !params.Capabilities.Workspace.Configuration {
		t.Fatal("workspace.configuration capability not advertised with settings present")
	}
}

func TestInitializeWithoutSettingsOmitsSettingsChannel(t *testing.T) {
	client, dump := startSettingsPeer(t)
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, raw := readDumpedInitialize(t, dump)
	if _, present := raw["initializationOptions"]; present {
		t.Fatalf("initializationOptions present without settings: %s", raw["initializationOptions"])
	}
	if workspace, err := json.Marshal(raw["capabilities"]); err == nil {
		if strings.Contains(string(workspace), "configuration") {
			t.Fatalf("workspace.configuration advertised without settings: %s", workspace)
		}
	}
}

func TestGetClientDeliversServerSettingsBeforeInitialize(t *testing.T) {
	cfg := config.DefaultConfig()
	goCfg := cfg.LSP["go"]
	goCfg.BuildFlags = []string{"-tags=fixturetag"}
	cfg.LSP["go"] = goCfg
	var delivered map[string]any
	oldNew, oldInit := newLSPClient, initializeLSP
	defer func() { newLSPClient, initializeLSP = oldNew, oldInit }()
	newLSPClient = func(string, []string, string, string) (*Client, error) { return &Client{}, nil }
	initializeLSP = func(_ context.Context, client *Client) error {
		delivered = client.ServerSettings()
		return nil
	}
	rootURI := (&url.URL{Scheme: "file", Path: t.TempDir()}).String()
	if _, err := NewManager(cfg, rootURI).GetClient(context.Background(), "go"); err != nil {
		t.Fatalf("get client: %v", err)
	}
	flags, ok := delivered["buildFlags"].([]string)
	if !ok || !slices.Equal([]string{"-tags=fixturetag"}, flags) {
		t.Fatalf("settings at initialize = %v, want buildFlags [-tags=fixturetag]", delivered)
	}
}

func TestGetClientDeliversNoSettingsWithoutBuildFlags(t *testing.T) {
	var delivered map[string]any
	oldNew, oldInit := newLSPClient, initializeLSP
	defer func() { newLSPClient, initializeLSP = oldNew, oldInit }()
	newLSPClient = func(string, []string, string, string) (*Client, error) { return &Client{}, nil }
	initializeLSP = func(_ context.Context, client *Client) error {
		delivered = client.ServerSettings()
		return nil
	}
	rootURI := (&url.URL{Scheme: "file", Path: t.TempDir()}).String()
	if _, err := NewManager(config.DefaultConfig(), rootURI).GetClient(context.Background(), "go"); err != nil {
		t.Fatalf("get client: %v", err)
	}
	if delivered != nil {
		t.Fatalf("settings delivered without configured build flags: %v", delivered)
	}
}
