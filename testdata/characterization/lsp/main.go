// This scripted protocol peer tests client transport and ownership only.
// It is not a real language server or evidence of semantic LSP compatibility.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func main() {
	mode := flag.String("mode", "normal", "normal, exit-on-probe, block-after-initialize, or ignore-exit")
	fixture := flag.String("responses", "", "method-to-result JSON fixture")
	flag.Parse()
	if err := serve(*mode, *fixture); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func serve(mode, fixture string) error {
	results := map[string]json.RawMessage{}
	if fixture != "" {
		data, err := os.ReadFile(fixture)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &results); err != nil {
			return err
		}
	}

	reader := bufio.NewReader(os.Stdin)
	rootURI := ""
	for {
		message, err := readMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if message.Method == "exit" {
			if mode == "ignore-exit" {
				hang()
			}
			return nil
		}
		if len(message.ID) == 0 {
			continue
		}
		if message.Method == "probe" && mode == "exit-on-probe" {
			return nil
		}

		key := message.Method
		var params struct {
			Position struct {
				Line int `json:"line"`
			} `json:"position"`
			Item struct {
				Name string `json:"name"`
			} `json:"item"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil && len(message.Params) > 0 {
			return err
		}
		if message.Method == "textDocument/references" || message.Method == "textDocument/prepareTypeHierarchy" {
			key = fmt.Sprintf("%s@%d", message.Method, params.Position.Line)
		} else if message.Method == "typeHierarchy/supertypes" {
			key += "@" + params.Item.Name
		}
		configured, ok := results[key]
		if !ok {
			configured, ok = results[message.Method]
		}
		result := json.RawMessage("null")
		if message.Method == "initialize" {
			var params struct {
				RootURI string `json:"rootUri"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil {
				return err
			}
			rootURI = params.RootURI
			result = json.RawMessage(`{"capabilities":{"documentSymbolProvider":true,"referencesProvider":true,"typeHierarchyProvider":true}}`)
		} else if ok {
			// Substitute inside encoded JSON, preserving escaping in the root URI.
			encoded, _ := json.Marshal(rootURI)
			escapedURI := string(encoded[1 : len(encoded)-1])
			result = json.RawMessage(strings.ReplaceAll(string(configured), "${ROOT_URI}", escapedURI))
		}
		response, err := json.Marshal(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Result  json.RawMessage `json:"result"`
		}{"2.0", message.ID, result})
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(response), response); err != nil {
			return err
		}
		if message.Method == "initialize" && mode == "block-after-initialize" {
			hang()
		}
	}
}

func readMessage(reader *bufio.Reader) (request, error) {
	var message request
	length := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return message, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if value, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return message, err
			}
		}
	}
	if length <= 0 || length > 16<<20 {
		return message, fmt.Errorf("invalid content length %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return message, err
	}
	err := json.Unmarshal(body, &message)
	return message, err
}

func hang() {
	// Deliberate peer misbehavior. The test owns termination, not this sleep.
	for {
		time.Sleep(time.Hour)
	}
}
