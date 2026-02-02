package registry

import (
	"time"
)

const (
	// RegistryVersion is the current version of the registry file format
	RegistryVersion = "1.0"
	// RegistryFile is the name of the registry file
	RegistryFile = "registry.json"
	// ConfigDirName is the name of the config directory
	ConfigDirName = ".codegraph"
)

// Project represents a metadata entry for a tracked project
type Project struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	InitializedAt time.Time `json:"initialized_at"`
	LastSeen      time.Time `json:"last_seen"`
}

// Registry represents the root structure of the registry file
type Registry struct {
	Version  string              `json:"version"`
	Projects map[string]*Project `json:"projects"`
}

// New creates a new empty Registry
func New() *Registry {
	return &Registry{
		Version:  RegistryVersion,
		Projects: make(map[string]*Project),
	}
}
