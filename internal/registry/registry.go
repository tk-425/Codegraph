package registry

import (
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

// Save writes the registry to disk
func (r *Registry) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode registry: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write registry file: %w", err)
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
