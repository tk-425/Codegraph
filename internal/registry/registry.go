package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultRegistryPath returns the default path for the registry file (~/.codegraph/registry.json)
func DefaultRegistryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(home, ConfigDirName, RegistryFile), nil
}

// Load reads the registry file from disk
func Load(path string) (*Registry, error) {
	// If file doesn't exist, return new empty registry
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return New(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry file: %w", err)
	}

	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse registry file: %w", err)
	}

	if reg.Projects == nil {
		reg.Projects = make(map[string]*Project)
	}

	return &reg, nil
}

// Update serializes a read-modify-publish operation against the latest registry.
// The callback must not retain r or recursively update the same registry.
func Update(ctx context.Context, path string, update func(r *Registry) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	lock, err := lockRegistry(ctx, path+".lock")
	if err != nil {
		return fmt.Errorf("failed to lock registry: %w", err)
	}
	// Keep the lock file: unlinking it would allow writers to lock different inodes.
	defer lock.Close()

	r, err := Load(path)
	if err != nil {
		return err
	}
	if err := update(r); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode registry: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return publish(ctx, path, data)
}

func publish(ctx context.Context, path string, data []byte) error {
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat registry: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".registry-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create registry candidate: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write registry candidate: %w", err)
	}
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("failed to set registry permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync registry candidate: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close registry candidate: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("failed to publish registry: %w", err)
	}
	return nil
}

// Add updates or creates a project entry
func (r *Registry) Add(path, name string) {
	path = filepath.Clean(path)
	now := time.Now()

	if proj, exists := r.Projects[path]; exists {
		proj.LastSeen = now
		// Update name if changed? Maybe keep original? Let's update it.
		proj.Name = name
	} else {
		r.Projects[path] = &Project{
			Name:          name,
			Path:          path,
			InitializedAt: now,
			LastSeen:      now,
		}
	}
}

// Remove deletes a project from the registry
func (r *Registry) Remove(path string) {
	delete(r.Projects, filepath.Clean(path))
}
