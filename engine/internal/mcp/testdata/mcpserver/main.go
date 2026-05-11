// Package main implements a minimal MCP server for integration testing.
// It reads JSON-RPC 2.0 lines from stdin and writes responses to stdout.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		// Notifications have no ID — do not respond.
		if req.ID == nil {
			continue
		}

		var id interface{}
		_ = json.Unmarshal(*req.ID, &id)

		resp := handleMethod(req.Method, req.Params, id)
		data, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", data)
	}
}

func handleMethod(method string, params json.RawMessage, id interface{}) response {
	switch method {
	case "initialize":
		return response{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "test-mcpserver", "version": "1.0.0"},
		}}
	case "tools/list":
		return response{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{
			"tools": []interface{}{
				map[string]interface{}{
					"name": "echo", "description": "Echoes input text",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"text": map[string]interface{}{"type": "string"},
						},
						"required": []string{"text"},
					},
				},
				map[string]interface{}{
					"name": "get_env", "description": "Returns an env var value",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name": map[string]interface{}{"type": "string"},
						},
						"required": []string{"name"},
					},
				},
			},
		}}
	case "tools/call":
		return handleToolCall(params, id)
	default:
		return response{JSONRPC: "2.0", ID: id, Error: map[string]interface{}{
			"code": -32601, "message": "method not found",
		}}
	}
}

func handleToolCall(params json.RawMessage, id interface{}) response {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return response{JSONRPC: "2.0", ID: id, Error: map[string]interface{}{
			"code": -32602, "message": "invalid params",
		}}
	}

	switch p.Name {
	case "echo":
		text, _ := p.Arguments["text"].(string)
		return toolResult(id, text, false)
	case "get_env":
		name, _ := p.Arguments["name"].(string)
		return toolResult(id, os.Getenv(name), false)
	case "slow_echo":
		time.Sleep(5 * time.Second)
		text, _ := p.Arguments["text"].(string)
		return toolResult(id, text, false)
	default:
		return toolResult(id, "unknown tool: "+p.Name, true)
	}
}

func toolResult(id interface{}, text string, isError bool) response {
	return response{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": text},
		},
		"isError": isError,
	}}
}
