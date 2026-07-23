package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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
