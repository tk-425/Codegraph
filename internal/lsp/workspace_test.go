package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Observe both messages through the framed peer, not the private folder helper.
func TestWorkspaceFoldersProtocol(t *testing.T) {
	// Decode with independent wire tags so a production tag typo cannot
	// pass by being shared between the sender and this assertion.
	type wireFolder struct {
		URI  string `json:"uri"`
		Name string `json:"name"`
	}
	for _, root := range []struct {
		name, directory string
	}{
		{"project", "project"},
		{"encoded", "workspace # % café"},
		{"absent", ""},
	} {
		for _, withSettings := range []bool{false, true} {
			t.Run(root.name+"/settings="+strconv.FormatBool(withSettings), func(t *testing.T) {
				rootURI := ""
				if root.directory != "" {
					rootPath := filepath.Join(t.TempDir(), root.directory)
					if err := os.Mkdir(rootPath, 0o755); err != nil {
						t.Fatal(err)
					}
					rootURI = PathToURI(rootPath)
				}
				if root.name == "encoded" {
					for _, escape := range []string{"%20", "%23", "%25", "%C3%A9"} {
						if !strings.Contains(rootURI, escape) {
							t.Fatalf("fixture URI %q lacks %s", rootURI, escape)
						}
					}
				}

				dumps := t.TempDir()
				initializeDump := filepath.Join(dumps, "initialize.json")
				workspaceDump := filepath.Join(dumps, "workspace.json")
				client, err := NewClient(lifecyclePeer(t), []string{
					"-mode", "request-workspace-folders",
					"-dump-init", initializeDump,
					"-dump-workspace", workspaceDump,
				}, rootURI, "go")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := client.Shutdown(context.Background()); err != nil {
						t.Errorf("shutdown peer: %v", err)
					}
				})
				if withSettings {
					client.SetServerSettings(map[string]any{"buildFlags": []string{"-tags=fixturetag"}})
				}
				ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
				defer cancel()
				if _, err := client.Initialize(ctx); err != nil {
					t.Fatalf("initialize peer: %v", err)
				}

				data, err := os.ReadFile(initializeDump)
				if err != nil {
					t.Fatal(err)
				}
				var params struct {
					RootURI          string       `json:"rootUri"`
					WorkspaceFolders []wireFolder `json:"workspaceFolders"`
					Capabilities     struct {
						Workspace struct {
							WorkspaceFolders bool `json:"workspaceFolders"`
							Configuration    bool `json:"configuration"`
						} `json:"workspace"`
					} `json:"capabilities"`
					InitializationOptions map[string]any `json:"initializationOptions"`
				}
				if err := json.Unmarshal(data, &params); err != nil {
					t.Fatal(err)
				}
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(data, &raw); err != nil {
					t.Fatal(err)
				}
				if _, present := raw["rootUri"]; !present {
					t.Fatal("legacy rootUri field omitted from initialize")
				}
				if params.RootURI != rootURI {
					t.Fatalf("legacy rootUri = %q, want %q", params.RootURI, rootURI)
				}
				if !params.Capabilities.Workspace.WorkspaceFolders {
					t.Fatal("workspaceFolders capability not advertised")
				}
				if params.Capabilities.Workspace.Configuration != withSettings {
					t.Fatalf("configuration capability = %t, settings present = %t", params.Capabilities.Workspace.Configuration, withSettings)
				}
				if withSettings {
					flags, ok := params.InitializationOptions["buildFlags"].([]any)
					if !ok || len(flags) != 1 || flags[0] != "-tags=fixturetag" {
						t.Fatalf("workspace initialization changed buildFlags: %v", params.InitializationOptions)
					}
				} else if len(params.InitializationOptions) != 0 {
					t.Fatalf("initialization options without settings: %v", params.InitializationOptions)
				}

				var expected []wireFolder
				if rootURI != "" {
					expected = []wireFolder{{URI: rootURI, Name: "workspace"}}
				}
				if !slices.Equal(params.WorkspaceFolders, expected) {
					t.Fatalf("initialize folders = %+v, want %+v", params.WorkspaceFolders, expected)
				}
				initialFolders, present := raw["workspaceFolders"]
				if !present {
					t.Fatal("workspaceFolders field omitted from initialize")
				}
				if rootURI == "" && string(bytes.TrimSpace(initialFolders)) != "null" {
					t.Fatalf("absent-root initialization = %s, want null", initialFolders)
				}

				response := bytes.TrimSpace(waitForConfigDump(t, workspaceDump))
				var folders []wireFolder
				if err := json.Unmarshal(response, &folders); err != nil {
					t.Fatalf("workspace response = %s: %v", response, err)
				}
				if !slices.Equal(folders, expected) || !slices.Equal(folders, params.WorkspaceFolders) {
					t.Fatalf("workspace reply %+v differs from initialize %+v or expected %+v", folders, params.WorkspaceFolders, expected)
				}
				if rootURI == "" && string(response) != "null" {
					t.Fatalf("absent-root response = %s, want null", response)
				}
			})
		}
	}
}
