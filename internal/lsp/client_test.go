package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestReadResponsesAnswersRegisterCapabilityRequest(t *testing.T) {
	serverReader, serverWriter := io.Pipe()
	clientReader, clientWriter := io.Pipe()
	client := &Client{
		stdin:   clientWriter,
		stdout:  io.NopCloser(strings.NewReader("")),
		reader:  bufio.NewReader(serverReader),
		pending: make(map[int64]chan *Response),
	}

	done := make(chan struct{})
	go func() {
		client.readResponses()
		close(done)
	}()

	request := []byte(`{"jsonrpc":"2.0","id":"ts1","method":"client/registerCapability","params":{"registrations":[]}}`)
	if _, err := fmt.Fprintf(serverWriter, "Content-Length: %d\r\n\r\n%s", len(request), request); err != nil {
		t.Fatal(err)
	}

	response, err := readTestLSPMessage(clientReader)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(response, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != "ts1" || string(decoded.Result) != "null" {
		t.Fatalf("response = %s", response)
	}

	_ = serverWriter.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not stop after server close")
	}
}

func readTestLSPMessage(reader io.Reader) ([]byte, error) {
	buffered := bufio.NewReader(reader)
	var contentLength int
	for {
		line, err := buffered.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if _, err := fmt.Sscanf(line, "Content-Length: %d", &contentLength); err != nil {
			continue
		}
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(buffered, body)
	return body, err
}

func TestNewClientStderrSink(t *testing.T) {
	tests := []struct {
		name        string
		passthrough bool
		want        any
	}{
		{name: "discards by default", passthrough: false, want: io.Discard},
		{name: "forwards under passthrough", passthrough: true, want: os.Stderr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := StderrPassthrough
			t.Cleanup(func() { StderrPassthrough = original })
			StderrPassthrough = test.passthrough
			t.Setenv(StderrPassthroughEnv, "")

			client, err := NewClient("true", nil, "file:///tmp", "go")
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			t.Cleanup(func() { client.stdin.Close() })

			if client.cmd.Stderr != test.want {
				t.Errorf("cmd.Stderr = %v, want %v", client.cmd.Stderr, test.want)
			}
		})
	}
}

// feedLogMessages pushes synthetic window/logMessage notifications through the
// real decode path and returns the records the client retained.
func feedLogMessages(t *testing.T, messages []LogMessageParams) []LSPDiagnostic {
	t.Helper()
	serverReader, serverWriter := io.Pipe()
	client := &Client{
		stdout:   io.NopCloser(strings.NewReader("")),
		reader:   bufio.NewReader(serverReader),
		pending:  make(map[int64]chan *Response),
		Language: "rust",
		cmd:      exec.Command("rust-analyzer"),
	}

	done := make(chan struct{})
	go func() {
		client.readResponses()
		close(done)
	}()

	for _, message := range messages {
		params, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		notification := fmt.Sprintf(`{"jsonrpc":"2.0","method":"window/logMessage","params":%s}`, params)
		if _, err := fmt.Fprintf(serverWriter, "Content-Length: %d\r\n\r\n%s", len(notification), notification); err != nil {
			t.Fatal(err)
		}
	}

	_ = serverWriter.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not stop after server close")
	}
	return client.DrainLogMessages()
}

func TestLogMessageRetainsErrorAndWarningOnly(t *testing.T) {
	tests := []struct {
		name         string
		messageType  MessageType
		wantRetained bool
		wantSeverity DiagnosticSeverity
	}{
		{name: "error retained", messageType: MessageTypeError, wantRetained: true, wantSeverity: SeverityError},
		{name: "warning retained", messageType: MessageTypeWarning, wantRetained: true, wantSeverity: SeverityWarning},
		{name: "info dropped", messageType: MessageTypeInfo},
		{name: "log dropped", messageType: MessageTypeLog},
		{name: "debug dropped", messageType: MessageTypeDebug},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			records := feedLogMessages(t, []LogMessageParams{{Type: test.messageType, Message: "something happened"}})
			if !test.wantRetained {
				if len(records) != 0 {
					t.Fatalf("got %d records, want none", len(records))
				}
				return
			}
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}
			record := records[0]
			if record.Severity != test.wantSeverity {
				t.Errorf("severity = %q, want %q", record.Severity, test.wantSeverity)
			}
			if record.Category != ServerLog {
				t.Errorf("category = %q, want %q", record.Category, ServerLog)
			}
			if record.Reason != "something happened" {
				t.Errorf("reason = %q, want the message text", record.Reason)
			}
			if record.Guidance != nil {
				t.Errorf("guidance = %+v, want none on a server-log record", record.Guidance)
			}
			if record.Language != "rust" {
				t.Errorf("language = %q, want rust", record.Language)
			}
		})
	}
}

func TestLogMessageDeduplicatesIdenticalText(t *testing.T) {
	messages := make([]LogMessageParams, 200)
	for i := range messages {
		messages[i] = LogMessageParams{Type: MessageTypeError, Message: "same problem"}
	}
	if records := feedLogMessages(t, messages); len(records) != 1 {
		t.Fatalf("got %d records, want 1 after de-duplication", len(records))
	}
}

func TestLogMessageStopsAtBufferCap(t *testing.T) {
	messages := make([]LogMessageParams, 200)
	for i := range messages {
		messages[i] = LogMessageParams{Type: MessageTypeWarning, Message: fmt.Sprintf("problem %d", i)}
	}
	if records := feedLogMessages(t, messages); len(records) != logMessageCap {
		t.Fatalf("got %d records, want the cap of %d", len(records), logMessageCap)
	}
}
