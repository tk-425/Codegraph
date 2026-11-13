package lsp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

// Manager handles the lifecycle of LSP servers
type Manager struct {
	cfg     *config.Config
	rootURI string

	mu          sync.Mutex
	clients     map[string]*Client // language -> client
	diagnostics map[string]LSPDiagnostic
	// unavailable caches a startup or transport failure for the rest of this
	// manager's build attempt, so later phases do not restart the server.
	unavailable map[string]error
	// servers records the server each GetClient resolution selected, even
	// when its startup then failed: the effective provider identity.
	servers map[string]ResolvedServer
}

const nativeTypeScriptMaxAttempts = 3

var (
	newLSPClient  = NewClient
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
		cfg:         cfg,
		rootURI:     rootURI,
		clients:     make(map[string]*Client),
		diagnostics: make(map[string]LSPDiagnostic),
		unavailable: make(map[string]error),
		servers:     make(map[string]ResolvedServer),
	}
}

// Server reports the server a GetClient resolution selected for language:
// its command, arguments and whether it is a native server. The identity
// is available even when startup then failed, because the refresh
// fingerprints the effective provider, not the successful one.
func (m *Manager) Server(language string) (command string, args []string, resolved bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	server, ok := m.servers[language]
	if !ok {
		return "", nil, false
	}
	return server.Command, server.Args, true
}

// GetClient gets or creates an LSP client for a language
func (m *Manager) GetClient(ctx context.Context, language string) (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err, ok := m.unavailable[language]; ok {
		return nil, err
	}
	if client, ok := m.clients[language]; ok {
		if err := client.Err(); err != nil {
			// A server whose transport failed is not restarted this attempt.
			delete(m.clients, language)
			_ = client.Close()
			m.unavailable[language] = err
			return nil, err
		}
		return client, nil
	}

	server, err := ResolveServer(m.cfg, projectRootFromURI(m.rootURI), language)
	if err != nil {
		m.diagnostics[language] = newDiagnostic(language, "", projectRootFromURI(m.rootURI), err, false)
		m.unavailable[language] = err
		return nil, err
	}
	// The effective server identity outlives the startup attempt: the
	// refresh fingerprints what was selected, succeeded or not.
	m.servers[language] = server

	attempts := 1
	if server.Native {
		attempts = nativeTypeScriptMaxAttempts
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		client, err := newLSPClient(server.Command, server.Args, m.rootURI, language)
		if err == nil {
			client.requestTimeout = m.cfg.LSPTimeouts.Request()
			client.gracefulTimeout = m.cfg.LSPTimeouts.ShutdownGraceful()
			client.forceTimeout = m.cfg.LSPTimeouts.ShutdownForce()
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

	diagnostic := newDiagnostic(language, server.Command, projectRootFromURI(m.rootURI), lastErr, m.cfg.IsExplicitLSPOverride(language))
	m.diagnostics[language] = diagnostic
	err = fmt.Errorf("failed to initialize LSP for %s: %w", language, lastErr)
	m.unavailable[language] = err
	return nil, err
}

// Diagnostics returns one startup-failure diagnostic per affected language,
// followed by the server log messages drained from every live client.
func (m *Manager) Diagnostics() []LSPDiagnostic {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]LSPDiagnostic, 0, len(m.diagnostics))
	for _, diagnostic := range m.diagnostics {
		out = append(out, diagnostic)
	}
	for _, client := range m.clients {
		logs := client.DrainLogMessages()
		if len(logs) > logMessageCap {
			logs = logs[:logMessageCap]
		}
		out = append(out, logs...)
	}
	return out
}

// cleanupFailedClient terminates a client whose initialization failed.
func cleanupFailedClient(client *Client) {
	if client.initialized {
		_ = shutdownLSP(context.Background(), client)
		return
	}
	_ = client.Close()
}

// ShutdownAll shuts down every LSP server, each within its own cleanup budget,
// and returns the joined cleanup failures.
func (m *Manager) ShutdownAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for _, client := range m.clients {
		if err := shutdownLSP(context.Background(), client); err != nil {
			errs = append(errs, err)
		}
	}
	m.clients = make(map[string]*Client)
	return errors.Join(errs...)
}

// ShutdownLanguage shuts down a specific language server
func (m *Manager) ShutdownLanguage(language string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, ok := m.clients[language]
	if !ok {
		return nil
	}
	delete(m.clients, language)
	return shutdownLSP(context.Background(), client)
}

// Executable returns the command of the running server for language, or "".
func (m *Manager) Executable(language string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if client, ok := m.clients[language]; ok {
		return client.Executable()
	}
	return ""
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
