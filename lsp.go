package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type lspClient struct {
	command      string
	cmd          *exec.Cmd
	conn         jsonrpc2.Conn
	server       protocol.Server
	callbacks    *lspCallbacks
	capabilities protocol.ServerCapabilities
	ctx          context.Context
	stderr       bytes.Buffer
	closed       bool
	waitOnce     sync.Once
	waitErr      error
}

type lspCallbacks struct {
	protocol.UnimplementedClient

	workspace protocol.WorkspaceFolder
	mu        sync.Mutex
	messages  []string
}

type processStdio struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	once   sync.Once
	err    error
}

func (c *processStdio) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *processStdio) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *processStdio) Close() error {
	c.once.Do(func() {
		c.err = errors.Join(c.stdin.Close(), c.stdout.Close())
	})
	return c.err
}

func startLSPClient(
	ctx context.Context,
	root string,
	command string,
	args ...string,
) (*lspClient, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("make workspace root absolute: %w", err)
	}
	rootURI := uri.File(absoluteRoot)
	workspace := protocol.WorkspaceFolder{
		URI:  rootURI,
		Name: filepath.Base(absoluteRoot),
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = absoluteRoot
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stdin: %w", command, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stdout: %w", command, err)
	}

	client := &lspClient{
		command:   command,
		cmd:       cmd,
		callbacks: &lspCallbacks{workspace: workspace},
	}
	cmd.Stderr = &client.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	stream := jsonrpc2.NewHeaderStream(&processStdio{stdin: stdin, stdout: stdout})
	client.ctx, client.conn, client.server = protocol.NewClient(ctx, client.callbacks, stream)

	supportsWorkspaceFolders := true
	supportsHierarchicalSymbols := true
	initializeResult, err := client.server.Initialize(client.ctx, &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{workspace}),
		},
		ProcessID: nil,
		ClientInfo: protocol.ClientInfo{
			Name:    "diffwhat",
			Version: protocol.NewOptional("dev"),
		},
		RootURI: &rootURI,
		Capabilities: protocol.ClientCapabilities{
			Workspace: &protocol.WorkspaceClientCapabilities{
				WorkspaceFolders: &supportsWorkspaceFolders,
			},
			TextDocument: &protocol.TextDocumentClientCapabilities{
				References: &protocol.ReferenceClientCapabilities{},
				DocumentSymbol: &protocol.DocumentSymbolClientCapabilities{
					HierarchicalDocumentSymbolSupport: &supportsHierarchicalSymbols,
				},
			},
			General: &protocol.GeneralClientCapabilities{
				PositionEncodings: []protocol.PositionEncodingKind{
					protocol.PositionEncodingKindUTF8,
					protocol.PositionEncodingKindUTF16,
				},
			},
		},
	})
	if err != nil {
		client.abort()
		return nil, fmt.Errorf("initialize %s: %w", command, err)
	}
	client.capabilities = initializeResult.Capabilities

	if err := client.server.Initialized(client.ctx, &protocol.InitializedParams{}); err != nil {
		client.abort()
		return nil, fmt.Errorf("notify %s initialization: %w", command, err)
	}
	return client, nil
}

func (c *lspClient) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true

	if err := c.server.Shutdown(c.ctx); err != nil {
		c.abort()
		return c.withStderr(err)
	}
	if err := c.server.Exit(c.ctx); err != nil {
		c.abort()
		return c.withStderr(err)
	}
	if err := c.wait(); err != nil {
		_ = c.conn.Close()
		return c.withStderr(fmt.Errorf("wait for %s: %w", c.command, err))
	}
	return c.conn.Close()
}

func (c *lspClient) abort() {
	c.closed = true
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.killAndWait()
}

func (c *lspClient) killAndWait() {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.wait()
}

func (c *lspClient) wait() error {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
	})
	return c.waitErr
}

func (c *lspClient) withStderr(err error) error {
	stderr := strings.TrimSpace(c.stderr.String())
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
}

func (c *lspClient) takeShowMessages() []string {
	c.callbacks.mu.Lock()
	defer c.callbacks.mu.Unlock()
	messages := c.callbacks.messages
	c.callbacks.messages = nil
	return messages
}

func (c *lspCallbacks) ShowMessage(
	_ context.Context,
	params *protocol.ShowMessageParams,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, params.Message)
	return nil
}

func (c *lspCallbacks) WorkspaceFolders(context.Context) ([]protocol.WorkspaceFolder, error) {
	return []protocol.WorkspaceFolder{c.workspace}, nil
}

func referencesProviderEnabled(provider protocol.ReferencesProvider) bool {
	switch provider := provider.(type) {
	case protocol.Boolean:
		return bool(provider)
	case *protocol.ReferenceOptions:
		return true
	default:
		return false
	}
}

func documentSymbolProviderEnabled(provider protocol.DocumentSymbolProvider) bool {
	switch provider := provider.(type) {
	case protocol.Boolean:
		return bool(provider)
	case *protocol.DocumentSymbolOptions:
		return true
	default:
		return false
	}
}
