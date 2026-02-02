package lsp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

// Manager handles the lifecycle of LSP servers
type Manager struct {
	cfg     *config.Config
	rootURI string
	
	mu      sync.Mutex
	clients map[string]*Client // language -> client
}

// NewManager creates a new LSP manager
func NewManager(cfg *config.Config, rootURI string) *Manager {
	return &Manager{
		cfg:     cfg,
		rootURI: rootURI,
		clients: make(map[string]*Client),
	}
}

// GetClient gets or creates an LSP client for a language
func (m *Manager) GetClient(ctx context.Context, language string) (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return existing client if available
	if client, ok := m.clients[language]; ok {
		return client, nil
	}

	// Get LSP config for language
	lspConfig, ok := m.cfg.LSP[language]
	if !ok {
		return nil, fmt.Errorf("no LSP configuration for language: %s", language)
	}

	// Create new client
	client, err := NewClient(lspConfig.Command, lspConfig.Args, m.rootURI, language)
	if err != nil {
		return nil, fmt.Errorf("failed to create LSP client for %s: %w", language, err)
	}

	// Initialize with timeout
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := client.Initialize(initCtx); err != nil {
		client.Shutdown(context.Background())
		return nil, fmt.Errorf("failed to initialize LSP for %s: %w", language, err)
	}

	m.clients[language] = client
	return client, nil
}

// ShutdownAll shuts down all LSP servers
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for lang, client := range m.clients {
		if err := client.Shutdown(ctx); err != nil {
			fmt.Printf("Warning: failed to shutdown %s LSP: %v\n", lang, err)
		}
	}
	m.clients = make(map[string]*Client)
}

// ShutdownLanguage shuts down a specific language server
func (m *Manager) ShutdownLanguage(language string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[language]; ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Shutdown(ctx)
		delete(m.clients, language)
	}
}

// ActiveLanguages returns list of active LSP languages
func (m *Manager) ActiveLanguages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	languages := make([]string, 0, len(m.clients))
	for lang := range m.clients {
		languages = append(languages, lang)
	}
	return languages
}

// IsAvailable checks if an LSP is configured for a language
func (m *Manager) IsAvailable(language string) bool {
	_, ok := m.cfg.LSP[language]
	return ok
}
