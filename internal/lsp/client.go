package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

// ErrTransportClosed reports that a client can no longer exchange messages:
// the server exited, sent a malformed frame, or a write was abandoned.
var ErrTransportClosed = errors.New("lsp transport closed")

// maxFrameBytes bounds a single server message.
const maxFrameBytes = 64 << 20

// Client is a JSON-RPC 2.0 client for LSP communication. It owns its server
// process: one reaper goroutine waits on it, and any transport failure kills it.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader

	writeMu     sync.Mutex
	mu          sync.Mutex
	nextID      int64
	pending     map[int64]chan *Response
	initialized bool

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error

	// exited is closed once the reaper's Wait returns.
	exited chan struct{}

	requestTimeout  time.Duration
	gracefulTimeout time.Duration
	forceTimeout    time.Duration

	// logMu guards the server log buffer separately from mu so the reader
	// goroutine never contends with the pending map or stdin writes.
	logMu       sync.Mutex
	logMessages []LSPDiagnostic
	logSeen     map[string]struct{}

	Language string
	RootURI  string
}

// Request represents a JSON-RPC 2.0 request
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError represents a JSON-RPC 2.0 error
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// methodNotFound is the JSON-RPC code a server returns for a request it does
// not support.
const methodNotFound = -32601

// IsUnsupported reports whether err is a server's "method not found" reply:
// an unsupported capability rather than a failed request.
func IsUnsupported(err error) bool {
	var responseErr *ResponseError
	return errors.As(err, &responseErr) && responseErr.Code == methodNotFound
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

// NewClient starts an LSP server and returns a client that owns it.
func NewClient(command string, args []string, rootURI, language string) (*Client, error) {
	cmd := exec.Command(command, args...)

	// Server standard error is discarded unless stderr passthrough is enabled.
	if StderrPassthroughEnabled() {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = io.Discard
	}

	// Own both pipe ends so Wait never closes the reader before it drains.
	childStdin, stdin, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stdout, childStdout, err := os.Pipe()
	if err != nil {
		_ = childStdin.Close()
		_ = stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	cmd.Stdin = childStdin
	cmd.Stdout = childStdout

	// The child holds its own copies; the parent keeps only its pipe ends.
	err = cmd.Start()
	_ = childStdin.Close()
	_ = childStdout.Close()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("failed to start LSP server: %w", err)
	}

	client := newClient(stdin, stdout, rootURI, language)
	client.cmd = cmd
	client.exited = make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(client.exited)
	}()
	go client.readResponses()

	return client, nil
}

// newClient starts with the default budgets; the manager applies configured ones.
func newClient(stdin io.WriteCloser, stdout io.ReadCloser, rootURI, language string) *Client {
	budgets := config.DefaultConfig().LSPTimeouts
	return &Client{
		stdin:           stdin,
		stdout:          stdout,
		reader:          bufio.NewReader(stdout),
		pending:         make(map[int64]chan *Response),
		closed:          make(chan struct{}),
		requestTimeout:  budgets.Request(),
		gracefulTimeout: budgets.ShutdownGraceful(),
		forceTimeout:    budgets.ShutdownForce(),
		Language:        language,
		RootURI:         rootURI,
	}
}

// Executable returns the server command path, or "" when there is none.
func (c *Client) Executable() string {
	if c.cmd == nil {
		return ""
	}
	return c.cmd.Path
}

// Err returns the transport failure once the client is closed, else nil.
func (c *Client) Err() error {
	select {
	case <-c.closed:
		return c.closeErr
	default:
		return nil
	}
}

// fail closes the transport once, failing every pending and future request
// and killing the owned server. The reaper still performs the only Wait.
func (c *Client) fail(cause error) {
	c.closeOnce.Do(func() {
		c.closeErr = fmt.Errorf("%w: %v", ErrTransportClosed, cause)
		if c.closed != nil { // nil only for zero-value test fakes
			close(c.closed)
		}
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		if c.stdout != nil {
			_ = c.stdout.Close()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
}

// logMessageCap bounds the retained server log messages per client.
const logMessageCap = 50

// recordLogMessage buffers a retained server log message, skipping text already
// seen for this language and stopping silently at the cap.
func (c *Client) recordLogMessage(severity DiagnosticSeverity, message string) {
	c.logMu.Lock()
	defer c.logMu.Unlock()

	if _, seen := c.logSeen[message]; seen {
		return
	}
	if len(c.logMessages) >= logMessageCap {
		return
	}
	if c.logSeen == nil {
		c.logSeen = make(map[string]struct{})
	}
	c.logSeen[message] = struct{}{}
	c.logMessages = append(c.logMessages, LSPDiagnostic{
		Language:   c.Language,
		Executable: c.Executable(),
		Category:   ServerLog,
		Severity:   severity,
		Reason:     message,
	})
}

// DrainLogMessages returns the buffered server log messages and clears the buffer.
func (c *Client) DrainLogMessages() []LSPDiagnostic {
	c.logMu.Lock()
	defer c.logMu.Unlock()

	drained := c.logMessages
	c.logMessages = nil
	return drained
}

// Initialize sends the initialize request to the LSP server. The caller's
// context is its only budget; initialization is not an ordinary request.
func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProcessID:    os.Getpid(),
		RootURI:      c.RootURI,
		Capabilities: DefaultClientCapabilities(),
	}

	var result InitializeResult
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return nil, err
	}

	// Send initialized notification
	if err := c.Notify("initialized", struct{}{}); err != nil {
		return nil, err
	}

	c.initialized = true
	return &result, nil
}

// Shutdown asks the server to exit within the graceful budget, then kills it
// and waits for the reap within the forced budget. An earlier ctx deadline
// shortens only the graceful phase; forced termination always runs.
func (c *Client) Shutdown(ctx context.Context) error {
	graceful, cancel := context.WithTimeout(ctx, c.gracefulTimeout)
	defer cancel()

	if c.initialized && c.Err() == nil {
		_ = c.call(graceful, "shutdown", nil, nil)
		_ = c.notify(graceful, "exit", nil)
	}
	if c.stdin != nil {
		// EOF tells a server that missed exit to stop.
		_ = c.stdin.Close()
	}
	select {
	case <-c.exited:
	case <-graceful.Done():
	}
	return c.Close()
}

// Close kills a server that is still running and waits for its reap within
// the forced budget. Shutdown calls it after the graceful phase.
func (c *Client) Close() error {
	killed := false
	if c.exited != nil {
		select {
		case <-c.exited:
		default:
			killed = true
		}
	}
	c.fail(errors.New("client closed"))
	if c.exited == nil {
		return nil
	}
	timer := time.NewTimer(c.forceTimeout)
	defer timer.Stop()
	select {
	case <-c.exited:
	case <-timer.C:
		return fmt.Errorf("%s server was not reaped within %s of being killed", c.Language, c.forceTimeout)
	}
	if killed {
		return fmt.Errorf("%s server was still running and was killed", c.Language)
	}
	return nil
}

// Call sends a request and waits for its response within the request budget.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	ctx, cancel := c.withRequestBudget(ctx)
	defer cancel()
	return c.call(ctx, method, params, result)
}

// withRequestBudget bounds ctx by the request budget; an earlier parent
// deadline still wins.
func (c *Client) withRequestBudget(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.requestTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.requestTimeout)
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	if err := c.Err(); err != nil {
		return err
	}
	id := atomic.AddInt64(&c.nextID, 1)

	req := Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Create response channel
	respChan := make(chan *Response, 1)
	c.mu.Lock()
	c.pending[id] = respChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.writeFrame(ctx, req); err != nil {
		return err
	}

	select {
	case resp := <-respChan:
		return decodeResponse(resp, result)
	case <-ctx.Done():
		go c.cancelRequest(id)
		return ctx.Err()
	case <-c.closed:
		// Prefer a response that arrived just before the transport closed.
		select {
		case resp := <-respChan:
			return decodeResponse(resp, result)
		default:
			return c.closeErr
		}
	}
}

func decodeResponse(resp *Response, result any) error {
	if resp.Error != nil {
		return resp.Error
	}
	if result != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

// cancelRequest tells the server a request was abandoned. It is best effort:
// skipped when another write is in progress, and bounded like any write so a
// server that stopped reading cannot hold the write lock indefinitely.
func (c *Client) cancelRequest(id int64) {
	if !c.writeMu.TryLock() {
		return
	}
	defer c.writeMu.Unlock()
	ctx, cancel := c.withRequestBudget(context.Background())
	defer cancel()
	_ = c.writeFrameLocked(ctx, Request{JSONRPC: "2.0", Method: "$/cancelRequest", Params: map[string]int64{"id": id}})
}

// Notify sends a notification (no response expected) within the request budget.
func (c *Client) Notify(method string, params any) error {
	ctx, cancel := c.withRequestBudget(context.Background())
	defer cancel()
	return c.notify(ctx, method, params)
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	return c.writeFrame(ctx, Request{JSONRPC: "2.0", Method: method, Params: params})
}

// writeFrame writes one message. If ctx ends mid-write the frame may be
// partial, so the transport is closed rather than left unframed.
func (c *Client) writeFrame(ctx context.Context, message any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrameLocked(ctx, message)
}

func (c *Client) writeFrameLocked(ctx context.Context, message any) error {
	if err := c.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.fail(ctx.Err()) })
	err := c.writeLocked(message)
	if !stop() {
		return ctx.Err()
	}
	if err != nil {
		c.fail(err)
		return c.closeErr
	}
	return nil
}

func (c *Client) writeLocked(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write body: %w", err)
	}
	return nil
}

// readResponses reads server messages until the transport fails, then fails
// every pending request with the cause.
func (c *Client) readResponses() {
	for {
		body, err := c.readFrame()
		if err != nil {
			c.fail(err)
			return
		}

		var message wireMessage
		if err := json.Unmarshal(body, &message); err != nil {
			c.fail(fmt.Errorf("malformed message body: %w", err))
			return
		}
		if message.Method == "window/logMessage" {
			var params LogMessageParams
			if json.Unmarshal(message.Params, &params) == nil {
				if severity, retained := SeverityForMessageType(params.Type); retained {
					c.recordLogMessage(severity, params.Message)
				}
			}
			continue
		}
		if message.Method != "" {
			// Only respond to server requests (have an ID), not notifications.
			// Notifications have no id field and expect no response; sending a
			// spurious error response with "id":null can confuse servers like
			// rust-analyzer, causing "malformed LSP payload" deserialization
			// errors and panics.
			if len(message.ID) > 0 && string(message.ID) != "null" {
				_ = c.respondToServerRequest(message.ID, message.Method)
			}
			continue
		}

		var id int64
		if err := json.Unmarshal(message.ID, &id); err != nil {
			continue
		}
		resp := Response{JSONRPC: message.JSONRPC, ID: id, Result: message.Result, Error: message.Error}

		// Dispatch to waiting caller; a duplicate response is dropped.
		if resp.ID > 0 {
			c.mu.Lock()
			if ch, ok := c.pending[resp.ID]; ok {
				select {
				case ch <- &resp:
				default:
				}
			}
			c.mu.Unlock()
		}
	}
}

// readFrame reads one Content-Length framed body.
func (c *Client) readFrame() ([]byte, error) {
	contentLength := -1
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break // End of headers
		}
		if value, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("malformed Content-Length %q", value)
			}
		}
	}
	if contentLength <= 0 {
		return nil, fmt.Errorf("missing or invalid Content-Length")
	}
	if contentLength > maxFrameBytes {
		return nil, fmt.Errorf("frame of %d bytes exceeds %d byte limit", contentLength, maxFrameBytes)
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) respondToServerRequest(id json.RawMessage, method string) error {
	var result any
	var responseErr *ResponseError
	switch method {
	case "client/registerCapability", "client/unregisterCapability":
		result = json.RawMessage("null")
	case "workspace/configuration":
		result = []any{}
	case "workspace/workspaceFolders":
		result = []map[string]string{{"uri": c.RootURI, "name": "workspace"}}
	default:
		responseErr = &ResponseError{Code: methodNotFound, Message: "method not found"}
	}

	response := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result,omitempty"`
		Error   *ResponseError  `json:"error,omitempty"`
	}{JSONRPC: "2.0", ID: id, Result: result, Error: responseErr}
	ctx, cancel := c.withRequestBudget(context.Background())
	defer cancel()
	return c.writeFrame(ctx, response)
}

// DocumentSymbols requests symbols from a document
func (c *Client) DocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	params := DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	var result []DocumentSymbol
	if err := c.Call(ctx, "textDocument/documentSymbol", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// DidOpenTextDocument notifies the server that a file has been opened
func (c *Client) DidOpenTextDocument(uri string, languageID string, content string) error {
	params := struct {
		TextDocument struct {
			URI        string `json:"uri"`
			LanguageID string `json:"languageId"`
			Version    int    `json:"version"`
			Text       string `json:"text"`
		} `json:"textDocument"`
	}{}
	params.TextDocument.URI = uri
	params.TextDocument.LanguageID = languageID
	params.TextDocument.Version = 1
	params.TextDocument.Text = content

	return c.Notify("textDocument/didOpen", params)
}

// DidCloseTextDocument notifies the server that a file has been closed
func (c *Client) DidCloseTextDocument(uri string) error {
	params := struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
	}{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}
	return c.Notify("textDocument/didClose", params)
}

// WorkspaceSymbols searches for symbols in the workspace
func (c *Client) WorkspaceSymbols(ctx context.Context, query string) ([]SymbolInformation, error) {
	params := WorkspaceSymbolParams{Query: query}

	var result []SymbolInformation
	if err := c.Call(ctx, "workspace/symbol", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// PrepareCallHierarchy prepares call hierarchy at a position
func (c *Client) PrepareCallHierarchy(ctx context.Context, uri string, pos Position) ([]CallHierarchyItem, error) {
	params := CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var result []CallHierarchyItem
	if err := c.Call(ctx, "textDocument/prepareCallHierarchy", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// IncomingCalls gets incoming calls for a call hierarchy item
func (c *Client) IncomingCalls(ctx context.Context, item CallHierarchyItem) ([]CallHierarchyIncomingCall, error) {
	params := CallHierarchyIncomingCallsParams{Item: item}

	var result []CallHierarchyIncomingCall
	if err := c.Call(ctx, "callHierarchy/incomingCalls", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// OutgoingCalls gets outgoing calls for a call hierarchy item
func (c *Client) OutgoingCalls(ctx context.Context, item CallHierarchyItem) ([]CallHierarchyOutgoingCall, error) {
	params := CallHierarchyOutgoingCallsParams{Item: item}

	var result []CallHierarchyOutgoingCall
	if err := c.Call(ctx, "callHierarchy/outgoingCalls", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// PrepareTypeHierarchy prepares type hierarchy at a position
func (c *Client) PrepareTypeHierarchy(ctx context.Context, uri string, pos Position) ([]TypeHierarchyItem, error) {
	params := TypeHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var result []TypeHierarchyItem
	if err := c.Call(ctx, "textDocument/prepareTypeHierarchy", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Supertypes gets supertypes for a type hierarchy item
func (c *Client) Supertypes(ctx context.Context, item TypeHierarchyItem) ([]TypeHierarchyItem, error) {
	params := TypeHierarchySupertypesParams{Item: item}

	var result []TypeHierarchyItem
	if err := c.Call(ctx, "typeHierarchy/supertypes", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Subtypes gets subtypes for a type hierarchy item
func (c *Client) Subtypes(ctx context.Context, item TypeHierarchyItem) ([]TypeHierarchyItem, error) {
	params := TypeHierarchySubtypesParams{Item: item}

	var result []TypeHierarchyItem
	if err := c.Call(ctx, "typeHierarchy/subtypes", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Implementation finds implementations of a symbol
func (c *Client) Implementation(ctx context.Context, uri string, pos Position) ([]Location, error) {
	params := ImplementationParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}

	var result []Location
	if err := c.Call(ctx, "textDocument/implementation", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// References finds all references to a symbol at a position
func (c *Client) References(ctx context.Context, uri string, pos Position, includeDeclaration bool) ([]Location, error) {
	params := struct {
		TextDocument TextDocumentIdentifier `json:"textDocument"`
		Position     Position               `json:"position"`
		Context      struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		} `json:"context"`
	}{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	params.Context.IncludeDeclaration = includeDeclaration

	var result []Location
	if err := c.Call(ctx, "textDocument/references", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}
