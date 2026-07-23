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

const nativeTypeScriptMaxAttempts = 3

var (
	newLSPClient = NewClient
	initializeLSP = func(ctx context.Context, client *Client) error {
		_, err := client.Initialize(ctx)
		return err
	}
	shutdownLSP = func(ctx context.Context, client *Client) error {
		return client.Shutdown(ctx)
	}
	sleepBeforeRetry = time.Sleep
	cleanupFailedLSP = cleanupFailedClient
)

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

	// Resolve the configured server. TypeScript and TypeScript React may use
	// the project-local native server when automatic configuration is active.
	lspConfig, ok := m.cfg.LSP[language]
	if !ok {
		return nil, fmt.Errorf("no LSP configuration for language: %s", language)
	}
	server := typeScriptServer{command: lspConfig.Command, args: lspConfig.Args}
	if language == "typescript" || language == "typescriptreact" {
		resolved, resolveErr := resolveTypeScriptServer(m.cfg, projectRootFromURI(m.rootURI), language)
		if resolveErr != nil {
			return nil, resolveErr
		}
		server = resolved
	}

	attempts := 1
	if server.native {
		attempts = nativeTypeScriptMaxAttempts
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		client, err := newLSPClient(server.command, server.args, m.rootURI, language)
		if err == nil {
			initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err = initializeLSP(initCtx, client)
			cancel()
		}
		if err == nil {
			m.clients[language] = client
			return client, nil
		}

		lastErr = err
		if client != nil {
			cleanupFailedLSP(client)
		}
		if attempt < attempts {
			sleepBeforeRetry(time.Duration(attempt) * 100 * time.Millisecond)
		}
	}

	return nil, fmt.Errorf("failed to initialize LSP for %s: %w", language, lastErr)
}

// ShutdownAll shuts down all LSP servers
func cleanupFailedClient(client *Client) {
	if client.initialized {
		_ = shutdownLSP(context.Background(), client)
		return
	}
	if client.stdin != nil {
		_ = client.stdin.Close()
	}
	if client.stdout != nil {
		_ = client.stdout.Close()
	}
	if client.cmd != nil {
		_ = client.cmd.Wait()
	}
}

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
