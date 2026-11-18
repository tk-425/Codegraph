//go:build darwin || linux

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDeduplicatePreviewApplyAndRepeat(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	alias := filepath.Join(dir, "alias")
	other := filepath.Join(dir, "distinct")
	for _, path := range []string{project, other} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)
	missing := filepath.Join(dir, "missing")
	reg := New()
	reg.Projects[project] = &Project{Name: "old", Path: project, InitializedAt: early, LastSeen: early}
	reg.Projects[alias] = &Project{Name: "latest", Path: alias, InitializedAt: late, LastSeen: late}
	reg.Projects[other] = &Project{Name: "other", Path: other, InitializedAt: early, LastSeen: early}
	reg.Projects[missing] = &Project{Name: "missing", Path: missing, InitializedAt: early, LastSeen: early}
	original, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	groups, err := Deduplicate(context.Background(), path, false)
	if err != nil || len(groups) != 1 {
		t.Fatalf("preview: groups=%+v err=%v", groups, err)
	}
	assertBytes := func(path string, want []byte) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s changed or unreadable: %v", path, err)
		}
	}
	assertBytes(path, original)
	backups, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 0 {
		t.Fatalf("preview created backups: %v %v", backups, err)
	}

	groups, err = Deduplicate(context.Background(), path, true)
	if err != nil || len(groups) != 1 {
		t.Fatalf("apply: groups=%+v err=%v", groups, err)
	}
	if groups[0].Project.Path != project || groups[0].Project.Name != "latest" ||
		!groups[0].Project.InitializedAt.Equal(early) || !groups[0].Project.LastSeen.Equal(late) {
		t.Fatalf("wrong merge: %+v", groups[0])
	}
	assertBytes(groups[0].Backup, original)
	backupInfo, err := os.Stat(groups[0].Backup)
	if err != nil || backupInfo.Mode().Perm() != 0o600 {
		t.Fatalf("backup permissions: %v %v", backupInfo, err)
	}
	repaired, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(repaired.Projects) != 3 || repaired.Projects[alias] != nil {
		t.Fatalf("wrong repaired registry: %+v", repaired)
	}
	for _, key := range []string{other, missing} {
		if !reflect.DeepEqual(repaired.Projects[key], reg.Projects[key]) {
			t.Fatalf("unrelated record changed: %s", key)
		}
	}
	published, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Deduplicate(context.Background(), path, true)
	if err != nil || len(again) != 0 {
		t.Fatalf("repeat: groups=%+v err=%v", again, err)
	}
	assertBytes(path, published)
	backups, err = filepath.Glob(path + ".backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("repeat created backup: %v %v", backups, err)
	}
}

func TestDeduplicateMalformedRegistryIsPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	original := []byte("not JSON")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Deduplicate(context.Background(), path, true); err == nil {
		t.Fatal("expected malformed registry error")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("malformed registry changed: %v", err)
	}
}
