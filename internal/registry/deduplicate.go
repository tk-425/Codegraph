package registry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// DuplicateGroup describes an explicit repair, not a change made by Load or Add.
type DuplicateGroup struct {
	Paths   []string `json:"paths"`
	Project Project  `json:"project"`
	Backup  string   `json:"backup,omitempty"`
}

// duplicateGroups only groups existing directories whose physical identity can
// be established. Missing, inaccessible and non-directory paths are untouched.
func duplicateGroups(r *Registry) []DuplicateGroup {
	keys := make([]string, 0, len(r.Projects))
	for key := range r.Projects {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	type directory struct {
		info  os.FileInfo
		paths []string
	}
	var directories []directory
	for _, key := range keys {
		info, err := os.Stat(key)
		if err != nil || !info.IsDir() || r.Projects[key] == nil {
			continue
		}
		found := false
		for i := range directories {
			if os.SameFile(info, directories[i].info) {
				directories[i].paths = append(directories[i].paths, key)
				found = true
				break
			}
		}
		if !found {
			directories = append(directories, directory{info: info, paths: []string{key}})
		}
	}
	groups := make([]DuplicateGroup, 0)
	for _, dir := range directories {
		if len(dir.paths) < 2 {
			continue
		}
		// Sorted paths break equal-timestamp ties deterministically.
		first, latest := dir.paths[0], dir.paths[0]
		for _, key := range dir.paths[1:] {
			if r.Projects[key].InitializedAt.Before(r.Projects[first].InitializedAt) {
				first = key
			}
			if r.Projects[key].LastSeen.After(r.Projects[latest].LastSeen) {
				latest = key
			}
		}
		merged := *r.Projects[first]
		merged.Path = first
		merged.Name = r.Projects[latest].Name
		merged.LastSeen = r.Projects[latest].LastSeen
		groups = append(groups, DuplicateGroup{Paths: dir.paths, Project: merged})
	}
	return groups
}

var errNoDuplicates = errors.New("no duplicate directories")

// Deduplicate previews repairs without writing, or recomputes and applies them
// under Update's lock after preserving the exact original bytes in a backup.
// A no-op apply does not publish or create a backup.
func Deduplicate(ctx context.Context, path string, apply bool) ([]DuplicateGroup, error) {
	if !apply {
		r, err := Load(path)
		if err != nil {
			return nil, err
		}
		return duplicateGroups(r), nil
	}
	groups := make([]DuplicateGroup, 0)
	err := Update(ctx, path, func(r *Registry) error {
		groups = duplicateGroups(r)
		if len(groups) == 0 {
			return errNoDuplicates
		}
		original, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read original registry for backup: %w", err)
		}
		backup, err := backupRegistry(path, original)
		if err != nil {
			return err
		}
		for i := range groups {
			group := &groups[i]
			group.Backup = backup
			for _, key := range group.Paths {
				delete(r.Projects, key)
			}
			merged := group.Project
			r.Projects[merged.Path] = &merged
		}
		return nil
	})
	if errors.Is(err, errNoDuplicates) {
		return groups, nil
	}
	if err != nil && len(groups) > 0 && groups[0].Backup != "" {
		return groups, fmt.Errorf("registry repair failed; original backup retained at %s: %w", groups[0].Backup, err)
	}
	return groups, err
}

func backupRegistry(path string, data []byte) (string, error) {
	// CreateTemp uses exclusive creation and 0600 permissions. This is a
	// persistent backup, deliberately not an unpublished candidate to clean up.
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".backup-*")
	if err != nil {
		return "", fmt.Errorf("create registry backup: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write registry backup %s (retained): %w", file.Name(), err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("sync registry backup %s (retained): %w", file.Name(), err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close registry backup %s (retained): %w", file.Name(), err)
	}
	return file.Name(), nil
}
