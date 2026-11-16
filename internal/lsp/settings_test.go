package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

// startSettingsPeer starts the scripted peer and returns the client plus the
// directory the peer's dump files live in. Extra args compose after the
// defaults; a repeated -mode overrides the earlier one.
func startSettingsPeer(t *testing.T, extraArgs ...string) (*Client, string) {
	t.Helper()
	root := t.TempDir()
	rootURI := (&url.URL{Scheme: "file", Path: root}).String()
	args := append([]string{"-mode", "normal", "-dump-init", filepath.Join(root, "init-params.json"), "-dump-config", filepath.Join(root, "config-response.json")}, extraArgs...)
	client, err := NewClient(lifecyclePeer(t), args, rootURI, "go")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil && !strings.Contains(err.Error(), "was killed") {
			t.Errorf("close: %v", err)
		}
	})
	return client, root
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
	client, root := startSettingsPeer(t)
	client.SetServerSettings(map[string]any{"buildFlags": []string{"-tags=characterization,integration"}})
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	params, _ := readDumpedInitialize(t, filepath.Join(root, "init-params.json"))
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
	client, root := startSettingsPeer(t)
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, raw := readDumpedInitialize(t, filepath.Join(root, "init-params.json"))
	if _, present := raw["initializationOptions"]; present {
		t.Fatalf("initializationOptions present without settings: %s", raw["initializationOptions"])
	}
	if workspace, err := json.Marshal(raw["capabilities"]); err == nil {
		if strings.Contains(string(workspace), "configuration") {
			t.Fatalf("workspace.configuration advertised without settings: %s", workspace)
		}
	}
}

// waitForConfigDump polls until the peer has dumped the client's
// configuration response, or fails at the setup budget.
func waitForConfigDump(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(lifecycleSetup)
	for {
		if data, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(data)) > 0 {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("peer did not report a configuration response: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConfigurationRequestReturnsSettingsPerItem(t *testing.T) {
	client, root := startSettingsPeer(t, "-mode", "request-configuration")
	client.SetServerSettings(map[string]any{"buildFlags": []string{"-tags=fixturetag"}})
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	// The peer asks for sections gopls and other after initialized; the
	// response arrives asynchronously on the reader goroutine.
	var results []json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(waitForConfigDump(t, filepath.Join(root, "config-response.json"))), &results); err != nil {
		t.Fatalf("configuration response is not an array: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %s, want one per item", results)
	}
	var goplsSection struct {
		BuildFlags []string `json:"buildFlags"`
	}
	if err := json.Unmarshal(results[0], &goplsSection); err != nil {
		t.Fatalf("gopls item = %s: %v", results[0], err)
	}
	if !slices.Equal([]string{"-tags=fixturetag"}, goplsSection.BuildFlags) {
		t.Fatalf("gopls section buildFlags = %v", goplsSection.BuildFlags)
	}
	if string(results[1]) != "null" {
		t.Fatalf("other section = %s, want null", results[1])
	}
}

func TestConfigurationRequestWithoutSettingsOmitsResult(t *testing.T) {
	client, root := startSettingsPeer(t, "-mode", "request-configuration")
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	// No settings: the pre-existing answer, an empty result array, is kept.
	if got := string(bytes.TrimSpace(waitForConfigDump(t, filepath.Join(root, "config-response.json")))); got != "[]" {
		t.Fatalf("configuration response = %s, want the unchanged empty array", got)
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
