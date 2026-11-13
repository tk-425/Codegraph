package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Default configuration directory
const DefaultConfigDir = ".codegraph"

// Config represents the codegraph configuration
type Config struct {
	LSP         map[string]LSPConfig `toml:"lsp"`
	LSPTimeouts LSPTimeoutsConfig    `toml:"lsp_timeouts"`
	Search      SearchConfig         `toml:"search"`
	Database    DatabaseConfig       `toml:"database"`
}

// LSPTimeoutsConfig bounds ordinary requests and per-server cleanup. Server
// initialization keeps its own fixed per-attempt budget.
type LSPTimeoutsConfig struct {
	RequestSeconds          int `toml:"request_seconds"`
	ShutdownGracefulSeconds int `toml:"shutdown_graceful_seconds"`
	ShutdownForceSeconds    int `toml:"shutdown_force_seconds"`
}

func (t LSPTimeoutsConfig) Request() time.Duration {
	return time.Duration(t.RequestSeconds) * time.Second
}

func (t LSPTimeoutsConfig) ShutdownGraceful() time.Duration {
	return time.Duration(t.ShutdownGracefulSeconds) * time.Second
}

func (t LSPTimeoutsConfig) ShutdownForce() time.Duration {
	return time.Duration(t.ShutdownForceSeconds) * time.Second
}

func (t LSPTimeoutsConfig) validate() error {
	for name, seconds := range map[string]int{
		"request_seconds":           t.RequestSeconds,
		"shutdown_graceful_seconds": t.ShutdownGracefulSeconds,
		"shutdown_force_seconds":    t.ShutdownForceSeconds,
	} {
		if seconds <= 0 {
			return fmt.Errorf("lsp_timeouts.%s must be positive, got %d", name, seconds)
		}
	}
	return nil
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
			"typescriptreact": {
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
		LSPTimeouts: LSPTimeoutsConfig{
			RequestSeconds:          30,
			ShutdownGracefulSeconds: 5,
			ShutdownForceSeconds:    5,
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
	if err := cfg.LSPTimeouts.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Save saves the configuration to the config file
func Save(projectRoot string, cfg *Config) error {
	return save(projectRoot, cfg, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

// SaveIfAbsent saves config only when its path does not exist. Existing paths
// are preserved and reported with an error matching os.ErrExist.
func SaveIfAbsent(projectRoot string, cfg *Config) error {
	return save(projectRoot, cfg, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
}

func save(projectRoot string, cfg *Config, flags int) error {
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

	f, err := os.OpenFile(configPath, flags, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// GetDatabasePath returns the absolute path to the database
// IsExplicitLSPOverride reports whether a language differs from its built-in server definition.
func (c *Config) IsExplicitLSPOverride(language string) bool {
	configured, ok := c.LSP[language]
	if !ok {
		return false
	}
	defaults := DefaultConfig().LSP
	baseline, ok := defaults[language]
	if !ok {
		return true
	}
	if configured.Command != baseline.Command || len(configured.Args) != len(baseline.Args) {
		return true
	}
	for i := range configured.Args {
		if configured.Args[i] != baseline.Args[i] {
			return true
		}
	}
	return false
}

func (c *Config) GetDatabasePath(projectRoot string) string {
	if filepath.IsAbs(c.Database.Path) {
		return c.Database.Path
	}
	return filepath.Join(projectRoot, c.Database.Path)
}
