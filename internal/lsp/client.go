package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Client is a JSON-RPC 2.0 client for LSP communication
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	reader  *bufio.Reader
	
	mu          sync.Mutex
	nextID      int64
	pending     map[int64]chan *Response
	initialized bool
	
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

// ResponseError represents a JSON-RPC 2.0 error
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

// NewClient creates a new LSP client
func NewClient(command string, args []string, rootURI, language string) (*Client, error) {
	cmd := exec.Command(command, args...)
	
	// Use filtered writer for all LSP servers to suppress noisy stderr
	cmd.Stderr = &filteredWriter{
		w:        os.Stderr,
		language: language,
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start LSP server: %w", err)
	}

	client := &Client{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		reader:   bufio.NewReader(stdout),
		pending:  make(map[int64]chan *Response),
		Language: language,
		RootURI:  rootURI,
	}

	// Start response reader goroutine
	go client.readResponses()

	return client, nil
}

// filteredWriter filters out warning lines from stderr
type filteredWriter struct {
	w        io.Writer
	language string
	buf      []byte
}

func (f *filteredWriter) Write(p []byte) (n int, err error) {
	// Buffer the input to handle line-by-line filtering
	f.buf = append(f.buf, p...)
	
	// Process complete lines
	for {
		idx := strings.IndexByte(string(f.buf), '\n')
		if idx == -1 {
			break
		}
		
		line := string(f.buf[:idx+1])
		f.buf = f.buf[idx+1:]
		
		// Skip Java warning lines for jdtls
		if f.language == "java" {
			if strings.Contains(line, "WARNING:") ||
				strings.Contains(line, "INFO:") ||
				strings.Contains(line, "sun.misc.Unsafe") ||
				strings.Contains(line, "incubator modules") ||
				strings.Contains(line, "spifly") ||
				strings.Contains(line, "logback") {
				continue
			}
		}
		
		// Skip OCaml dune/merlin messages for ocamllsp
		if f.language == "ocaml" {
			if strings.Contains(line, "halting dune") ||
				strings.Contains(line, "closed merlin") ||
				strings.Contains(line, "{ pid") ||
				strings.Contains(line, "; initial_cwd") ||
				strings.HasPrefix(strings.TrimSpace(line), "\"") ||
				strings.TrimSpace(line) == "}" {
				continue
			}
		}
		
		// Skip rust-analyzer "unknown request" messages
		if f.language == "rust" {
			if strings.Contains(line, "ERROR unknown request") ||
				strings.Contains(line, "prepareTypeHierarchy") ||
				strings.Contains(line, "supertypes") ||
				strings.Contains(line, "subtypes") {
				continue
			}
		}
		
		// Write non-filtered lines
		f.w.Write([]byte(line))
	}
	
	return len(p), nil
}

// Initialize sends the initialize request to the LSP server
func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProcessID:    os.Getpid(),
		RootURI:      c.RootURI,
		Capabilities: DefaultClientCapabilities(),
	}

	var result InitializeResult
	if err := c.Call(ctx, "initialize", params, &result); err != nil {
		return nil, err
	}

	// Send initialized notification
	if err := c.Notify("initialized", struct{}{}); err != nil {
		return nil, err
	}

	c.initialized = true
	return &result, nil
}

// Shutdown sends shutdown request and exit notification
func (c *Client) Shutdown(ctx context.Context) error {
	if !c.initialized {
		return nil
	}

	// Send shutdown request
	var result any
	if err := c.Call(ctx, "shutdown", nil, &result); err != nil {
		// Ignore errors on shutdown
	}

	// Send exit notification
	c.Notify("exit", nil)

	// Close pipes and wait for process
	c.stdin.Close()
	c.stdout.Close()
	c.cmd.Wait()

	return nil
}

// Call sends a request and waits for response
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
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

	// Send request
	if err := c.send(req); err != nil {
		return err
	}

	// Wait for response
	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-respChan:
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	}
}

// Notify sends a notification (no response expected)
func (c *Client) Notify(method string, params any) error {
	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.send(req)
}

// send writes a request to the LSP server
func (c *Client) send(req Request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if _, err := io.WriteString(c.stdin, header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write body: %w", err)
	}

	return nil
}

// readResponses reads responses from the LSP server
func (c *Client) readResponses() {
	for {
		// Read headers
		contentLength := 0
		for {
			line, err := c.reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break // End of headers
			}
			if strings.HasPrefix(line, "Content-Length:") {
				lenStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
				contentLength, _ = strconv.Atoi(lenStr)
			}
		}

		if contentLength == 0 {
			continue
		}

		// Read body
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(c.reader, body); err != nil {
			return
		}

		// Parse response
		var resp Response
		if err := json.Unmarshal(body, &resp); err != nil {
			continue
		}

		// Dispatch to waiting caller
		if resp.ID > 0 {
			c.mu.Lock()
			if ch, ok := c.pending[resp.ID]; ok {
				ch <- &resp
			}
			c.mu.Unlock()
		}
	}
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
