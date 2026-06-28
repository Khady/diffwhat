package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type lspClient struct {
	command string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  bytes.Buffer
	rootURI string
	nextID  int
	mu      sync.Mutex
	closed  bool

	capabilities lspServerCapabilities
	showMessages []string
}

type lspResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type lspEnvelope struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      json.RawMessage   `json:"id,omitempty"`
	Method  string            `json:"method,omitempty"`
	Params  json.RawMessage   `json:"params,omitempty"`
	Result  json.RawMessage   `json:"result,omitempty"`
	Error   *lspResponseError `json:"error,omitempty"`
}

type lspServerCapabilities struct {
	PositionEncoding       string          `json:"positionEncoding"`
	DocumentSymbolProvider json.RawMessage `json:"documentSymbolProvider"`
	ReferencesProvider     json.RawMessage `json:"referencesProvider"`
}

func startLSPClient(
	ctx context.Context,
	root string,
	command string,
	args ...string,
) (*lspClient, error) {
	rootURI, err := fileURI(root)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stdin: %w", command, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stdout: %w", command, err)
	}

	client := &lspClient{
		command: command,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		rootURI: rootURI,
	}
	cmd.Stderr = &client.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	var initializeResult struct {
		Capabilities lspServerCapabilities `json:"capabilities"`
	}
	initializeParams := map[string]any{
		"processId": nil,
		"clientInfo": map[string]any{
			"name":    "diffwhat",
			"version": "dev",
		},
		"rootPath": root,
		"rootUri":  rootURI,
		"workspaceFolders": []map[string]string{{
			"uri":  rootURI,
			"name": filepath.Base(root),
		}},
		"capabilities": map[string]any{
			"general": map[string]any{
				"positionEncodings": []string{"utf-8", "utf-16"},
			},
			"textDocument": map[string]any{
				"documentSymbol": map[string]any{
					"hierarchicalDocumentSymbolSupport": true,
				},
			},
			"workspace": map[string]any{
				"workspaceFolders": true,
			},
		},
	}
	if err := client.request("initialize", initializeParams, &initializeResult); err != nil {
		client.abort()
		return nil, fmt.Errorf("initialize %s: %w", command, err)
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		client.abort()
		return nil, fmt.Errorf("notify %s initialization: %w", command, err)
	}
	client.capabilities = initializeResult.Capabilities
	return client, nil
}

func (c *lspClient) request(method string, params, result any) error {
	c.nextID++
	id := c.nextID
	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		message["params"] = params
	}
	if err := c.write(message); err != nil {
		return err
	}

	for {
		message, err := c.read()
		if err != nil {
			return c.withStderr(err)
		}
		if message.Method != "" {
			if len(message.ID) != 0 && string(message.ID) != "null" {
				if err := c.handleServerRequest(message); err != nil {
					return err
				}
			} else {
				c.handleNotification(message)
			}
			continue
		}

		var responseID int
		if err := json.Unmarshal(message.ID, &responseID); err != nil {
			return fmt.Errorf("decode %s response id: %w", method, err)
		}
		if responseID != id {
			return fmt.Errorf("received response %d while waiting for %s request %d", responseID, method, id)
		}
		if message.Error != nil {
			return fmt.Errorf(
				"%s failed with code %d: %s",
				method,
				message.Error.Code,
				message.Error.Message,
			)
		}
		if result == nil || len(message.Result) == 0 || string(message.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(message.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	}
}

func (c *lspClient) handleNotification(message lspEnvelope) {
	if message.Method != "window/showMessage" {
		return
	}
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(message.Params, &params); err == nil && params.Message != "" {
		c.showMessages = append(c.showMessages, params.Message)
	}
}

func (c *lspClient) notify(method string, params any) error {
	message := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		message["params"] = params
	}
	return c.write(message)
}

func (c *lspClient) handleServerRequest(message lspEnvelope) error {
	var result any
	var responseError *lspResponseError

	switch message.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return fmt.Errorf("decode workspace/configuration request: %w", err)
		}
		result = make([]any, len(params.Items))
	case "workspace/workspaceFolders":
		result = []map[string]string{{
			"uri":  c.rootURI,
			"name": filepath.Base(c.cmd.Dir),
		}}
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
		result = nil
	case "workspace/applyEdit":
		result = map[string]any{"applied": false}
	default:
		responseError = &lspResponseError{
			Code:    -32601,
			Message: "method not supported by diffwhat",
		}
	}

	response := map[string]any{
		"jsonrpc": "2.0",
		"id":      message.ID,
	}
	if responseError == nil {
		response["result"] = result
	} else {
		response["error"] = responseError
	}
	return c.write(response)
}

func (c *lspClient) write(message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode LSP message: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return fmt.Errorf("write LSP header: %w", err)
	}
	if _, err := c.stdin.Write(payload); err != nil {
		return fmt.Errorf("write LSP payload: %w", err)
	}
	return nil
}

func (c *lspClient) read() (lspEnvelope, error) {
	contentLength := -1
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return lspEnvelope{}, fmt.Errorf("read LSP header: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return lspEnvelope{}, fmt.Errorf("invalid LSP header %q", line)
		}
		if strings.EqualFold(name, "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return lspEnvelope{}, fmt.Errorf("invalid LSP content length %q", value)
			}
		}
	}
	if contentLength < 0 {
		return lspEnvelope{}, fmt.Errorf("LSP message has no Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, payload); err != nil {
		return lspEnvelope{}, fmt.Errorf("read LSP payload: %w", err)
	}
	var message lspEnvelope
	if err := json.Unmarshal(payload, &message); err != nil {
		return lspEnvelope{}, fmt.Errorf("decode LSP message: %w", err)
	}
	return message, nil
}

func (c *lspClient) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true

	if err := c.request("shutdown", nil, nil); err != nil {
		c.abort()
		return err
	}
	if err := c.notify("exit", nil); err != nil {
		c.abort()
		return err
	}
	if err := c.stdin.Close(); err != nil {
		c.abort()
		return fmt.Errorf("close %s stdin: %w", c.command, err)
	}
	if err := c.cmd.Wait(); err != nil {
		return c.withStderr(fmt.Errorf("wait for %s: %w", c.command, err))
	}
	return nil
}

func (c *lspClient) abort() {
	c.closed = true
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

func (c *lspClient) withStderr(err error) error {
	stderr := strings.TrimSpace(c.stderr.String())
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
}

func capabilityEnabled(capability json.RawMessage) bool {
	if len(capability) == 0 || string(capability) == "null" || string(capability) == "false" {
		return false
	}
	return true
}

func (c *lspClient) takeShowMessages() []string {
	messages := c.showMessages
	c.showMessages = nil
	return messages
}

func fileURI(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("make %s absolute: %w", path, err)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String(), nil
}

func pathFromFileURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse reference URI %q: %w", uri, err)
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported reference URI %q", uri)
	}
	path := filepath.FromSlash(parsed.Path)
	if parsed.Host != "" {
		path = string(filepath.Separator) + string(filepath.Separator) + parsed.Host + path
	}
	return path, nil
}
