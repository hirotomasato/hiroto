package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// MCPClient connects to a Model Context Protocol server over stdio.
type MCPClient struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID int
	tools  []ToolDef
}

// MCPServerConfig describes one MCP server to connect to.
type MCPServerConfig struct {
	Command string   `yaml:"command"` // e.g. "npx"
	Args    []string `yaml:"args"`    // e.g. ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
}

// ConnectMCP starts an MCP server and discovers its tools.
func ConnectMCP(ctx context.Context, cfg MCPServerConfig) (*MCPClient, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp start: %w", err)
	}
	mcp := &MCPClient{
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdin),
		stdout: bufio.NewReader(stdout),
	}

	// Initialize
	if _, err := mcp.call("initialize", map[string]any{
		"protocolVersion": "0.1.0",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "hiroto", "version": "0.3.0"},
	}); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("mcp init: %w", err)
	}

	// Discover tools
	result, err := mcp.call("tools/list", nil)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}

	var listResp struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResp); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("mcp parse tools: %w", err)
	}

	for _, t := range listResp.Tools {
		name := t.Name
		desc := t.Description
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		mcp.tools = append(mcp.tools, ToolDef{
			Name:        name,
			Description: desc,
			Parameters:  schema,
			Exec: func(ctx context.Context, args map[string]any) (string, bool) {
				return mcp.callTool(name, args)
			},
		})
	}
	return mcp, nil
}

func (m *MCPClient) callTool(name string, args map[string]any) (string, bool) {
	result, err := m.call("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return fmt.Sprintf("mcp error: %v", err), true
	}
	// Parse result — could be text or structured content
	var callResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &callResp); err == nil {
		var texts []string
		for _, c := range callResp.Content {
			if c.Type == "text" {
				texts = append(texts, c.Text)
			}
		}
		return strings.Join(texts, "\n"), callResp.IsError
	}
	return string(result), false
}

func (m *MCPClient) call(method string, params any) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	data, _ := json.Marshal(req)
	if _, err := m.stdin.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	m.stdin.Flush()
	line, err := m.stdout.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp: %s", resp.Error.Message)
	}
	return resp.Result, nil
}

// Tools returns all discovered MCP tools.
func (m *MCPClient) Tools() []ToolDef { return m.tools }

// Close terminates the MCP server process.
func (m *MCPClient) Close() error {
	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Kill()
	}
	return nil
}
