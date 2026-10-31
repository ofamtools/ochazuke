package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Versions match matomeru's server and the stable MCP lifecycle specification.
var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve uses the bounded newline JSON-RPC loop from matomeru, with lifecycle
// validation and negotiated structured results. EOF ends a stdio session.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	enc := json.NewEncoder(out)
	protocol := ""
	ready := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			code, message := -32700, "parse error"
			if json.Valid([]byte(line)) {
				code, message = -32600, "invalid request"
			}
			if err = enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{code, message}}); err != nil {
				return err
			}
			continue
		}
		notification := len(req.ID) == 0
		validID := len(req.ID) > 0 && string(req.ID) != "null" && (req.ID[0] == '"' || req.ID[0] == '-' || req.ID[0] >= '0' && req.ID[0] <= '9')
		var result any
		var rpcErr *rpcError
		switch {
		case req.JSONRPC != "2.0" || req.Method == "" || (!notification && !validID):
			rpcErr = &rpcError{-32600, "invalid request"}
		case req.Method == "initialize":
			if notification || protocol != "" {
				rpcErr = &rpcError{-32600, "initialize must be the first request"}
				break
			}
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil || p.ProtocolVersion == "" {
				rpcErr = &rpcError{-32602, "protocolVersion is required"}
				break
			}
			protocol = protocolVersions[0]
			if slices.Contains(protocolVersions, p.ProtocolVersion) {
				protocol = p.ProtocolVersion
			}
			result = map[string]any{"protocolVersion": protocol, "serverInfo": map[string]string{"name": s.opts.Name, "version": s.opts.Version}, "capabilities": map[string]any{"tools": map[string]bool{"listChanged": false}, "resources": map[string]bool{"listChanged": false}, "prompts": map[string]bool{"listChanged": false}}, "instructions": s.opts.Instructions}
		case req.Method == "ping":
			result = map[string]any{}
		case req.Method == "notifications/initialized" && protocol != "":
			ready = true
		case !ready:
			rpcErr = &rpcError{-32000, "server is not initialized"}
		default:
			result, rpcErr = s.handle(ctx, req, protocol)
		}
		if notification && rpcErr == nil {
			continue
		}
		if notification && req.JSONRPC == "2.0" && req.Method != "" {
			continue
		}
		id := req.ID
		if !validID {
			id = json.RawMessage("null")
		}
		if err := enc.Encode(response{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, req request, protocol string) (any, *rpcError) {
	switch req.Method {
	case "notifications/cancelled", "notifications/roots/list_changed":
		return nil, nil
	case "tools/list":
		return map[string]any{"tools": s.Tools()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := decode(req.Params, &p); err != nil || p.Name == "" {
			return nil, &rpcError{-32602, "invalid tool arguments"}
		}
		arguments := map[string]any{}
		if len(p.Arguments) > 0 {
			if p.Arguments[0] != '{' {
				return nil, &rpcError{-32602, "arguments must be an object"}
			}
			if err := json.Unmarshal(p.Arguments, &arguments); err != nil {
				return nil, &rpcError{-32602, err.Error()}
			}
		}
		result, err := s.Call(ctx, p.Name, arguments)
		var bad *argumentError
		if errors.As(err, &bad) {
			return nil, &rpcError{-32602, err.Error()}
		}
		if err != nil {
			return map[string]any{"content": []any{map[string]string{"type": "text", "text": DataNote}, map[string]string{"type": "text", "text": err.Error()}}, "isError": true}, nil
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, &rpcError{-32603, err.Error()}
		}
		content := []any{map[string]string{"type": "text", "text": DataNote}, map[string]string{"type": "text", "text": string(raw)}}
		reply := map[string]any{"content": content, "isError": false}
		if m, ok := result.(map[string]any); ok {
			if status, _ := m["status"].(string); status == "refused" || status == "failed" {
				reply["isError"] = true
			}
		}
		if protocol >= "2025-06-18" {
			var generic any
			if err := json.Unmarshal(raw, &generic); err != nil {
				return nil, &rpcError{-32603, err.Error()}
			}
			if m, ok := generic.(map[string]any); ok {
				reply["structuredContent"] = m
			} else {
				reply["structuredContent"] = map[string]any{"result": generic}
			}
		}
		return reply, nil
	case "resources/list":
		return map[string]any{"resources": []any{map[string]string{"uri": "ochazuke://catalog", "name": "catalog", "mimeType": "application/json"}, map[string]string{"uri": "ochazuke://instructions", "name": "instructions", "mimeType": "text/plain"}}}, nil
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": []any{}}, nil
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		text := s.opts.Instructions
		mime := "text/plain"
		switch p.URI {
		case "ochazuke://catalog":
			v, err := s.backend.Call(ctx, "catalog", map[string]any{"include": "all", "limit": 1000})
			if err != nil {
				return nil, &rpcError{-32603, err.Error()}
			}
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, &rpcError{-32603, err.Error()}
			}
			text = string(raw)
			mime = "application/json"
		case "ochazuke://instructions":
		default:
			return nil, &rpcError{-32002, "unknown resource"}
		}
		return map[string]any{"contents": []any{map[string]string{"uri": p.URI, "mimeType": mime, "text": text}}}, nil
	case "prompts/list":
		return map[string]any{"prompts": []any{map[string]any{"name": "guide", "description": "How to discover and run this app's verbs", "arguments": []any{}}}}, nil
	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := decode(req.Params, &p); err != nil || p.Name != "guide" || len(p.Arguments) > 0 {
			return nil, &rpcError{-32602, "unknown prompt or arguments"}
		}
		return map[string]any{"description": "App catalog guide", "messages": []any{map[string]any{"role": "user", "content": map[string]string{"type": "text", "text": s.opts.Instructions}}}}, nil
	}
	return nil, &rpcError{-32601, fmt.Sprintf("method %q not found", req.Method)}
}

func decode(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}
