package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Default configuration directory
const DefaultConfigDir = ".codegraph"

// Config represents the codegraph configuration
type Config struct {
	LSP      map[string]LSPConfig `toml:"lsp"`
	Search   SearchConfig         `toml:"search"`
	Database DatabaseConfig       `toml:"database"`
}

// LSPConfig represents an LSP server configuration
type LSPConfig struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
}

// SearchConfig represents search configuration
type SearchConfig struct {
	TimeoutSeconds int `toml:"timeout_seconds"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
	Path string `toml:"path"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		LSP: map[string]LSPConfig{
			"go": {
				Command: "gopls",
				Args:    []string{"serve"},
			},
			"python": {
				Command: "pyright-langserver",
				Args:    []string{"--stdio"},
			},
			"typescript": {
				Command: "typescript-language-server",
				Args:    []string{"--stdio"},
			},
			"java": {
				Command: "jdtls",
				Args:    []string{"-data", "/tmp/jdtls-workspace"},
			},
			"swift": {
				Command: "sourcekit-lsp",
				Args:    []string{},
			},
			"rust": {
				Command: "rust-analyzer",
				Args:    []string{},
			},
			"ocaml": {
				Command: "ocamllsp",
				Args:    []string{},
			},
		},
		Search: SearchConfig{
			TimeoutSeconds: 30,
		},
		Database: DatabaseConfig{
			Path: ".codegraph/graphs/codegraph.db",
		},
	}
}

// Load loads the configuration from the config file
func Load(projectRoot string) (*Config, error) {
	configPath := filepath.Join(projectRoot, DefaultConfigDir, "config.toml")

	// If config doesn't exist, return default config
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return cfg, nil
}

// Save saves the configuration to the config file
func Save(projectRoot string, cfg *Config) error {
	configDir := filepath.Join(projectRoot, DefaultConfigDir)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.toml")

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	header := []byte("# CodeGraph Configuration\n# Languages are auto-detected based on file extensions in the project.\n# Only configure LSP commands if you need to override defaults.\n\n")
	data = append(header, data...)

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// GetDatabasePath returns the absolute path to the database
func (c *Config) GetDatabasePath(projectRoot string) string {
	if filepath.IsAbs(c.Database.Path) {
		return c.Database.Path
	}
	return filepath.Join(projectRoot, c.Database.Path)
}
