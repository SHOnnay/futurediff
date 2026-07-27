// Package mcpbridge exposes the FutureDiff orchestration API as a conservative
// MCP stdio server. It intentionally omits approval and commit tools: agents
// may prepare and verify a future, while a separate trusted user path releases
// persistent effects.
package mcpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/SHOnnay/futurediff/internal/api"
)

const ProtocolVersion = "2025-11-25"

type DaemonClient interface {
	Do(method, path string, body any) (json.RawMessage, error)
}

type Server struct {
	Client      DaemonClient
	Name        string
	Version     string
	initialized bool
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func New(socket string) *Server {
	return &Server{Client: api.NewClient(socket), Name: "futurediff", Version: "0.23.0"}
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if s.Client == nil {
		return errors.New("daemon client is required")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	writer := bufio.NewWriter(out)
	defer writer.Flush()
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := writeResponse(writer, response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}}); err != nil {
				return err
			}
			continue
		}
		resp, send := s.handle(req)
		if send {
			if err := writeResponse(writer, resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func writeResponse(w *bufio.Writer, r response) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if bytesContainNewline(b) {
		return errors.New("MCP response contained embedded newline")
	}
	if _, err = w.Write(append(b, '\n')); err != nil {
		return err
	}
	return w.Flush()
}
func bytesContainNewline(b []byte) bool {
	for _, v := range b {
		if v == '\n' || v == '\r' {
			return true
		}
	}
	return false
}

func (s *Server) handle(req request) (response, bool) {
	base := response{JSONRPC: "2.0", ID: req.ID}
	if req.JSONRPC != "2.0" || req.Method == "" {
		base.Error = &rpcError{Code: -32600, Message: "Invalid Request"}
		return base, len(req.ID) > 0
	}
	notification := len(req.ID) == 0
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    json.RawMessage `json:"capabilities"`
			ClientInfo      json.RawMessage `json:"clientInfo"`
		}
		if err := strict(req.Params, &p); err != nil {
			base.Error = &rpcError{Code: -32602, Message: "Invalid initialize parameters", Data: err.Error()}
			return base, !notification
		}
		if p.ProtocolVersion != ProtocolVersion {
			base.Error = &rpcError{Code: -32602, Message: "Unsupported protocol version", Data: map[string]any{"supported": ProtocolVersion}}
			return base, !notification
		}
		base.Result = map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": value(s.Name, "futurediff"), "version": value(s.Version, "0.13.0"), "description": "Transactional staging and verification for autonomous agents"}, "instructions": "Agents may stage and verify futures. Approval and commit are intentionally unavailable through MCP."}
		return base, !notification
	case "notifications/initialized":
		s.initialized = true
		return base, false
	case "ping":
		base.Result = map[string]any{}
		return base, !notification
	}
	if !s.initialized {
		base.Error = &rpcError{Code: -32002, Message: "Server is not initialized"}
		return base, !notification
	}
	switch req.Method {
	case "tools/list":
		base.Result = map[string]any{"tools": tools()}
		return base, !notification
	case "tools/call":
		var p callParams
		if err := strict(req.Params, &p); err != nil {
			base.Error = &rpcError{Code: -32602, Message: "Invalid tool call", Data: err.Error()}
			return base, !notification
		}
		result := s.call(p)
		base.Result = result
		return base, !notification
	default:
		base.Error = &rpcError{Code: -32601, Message: "Method not found"}
		return base, !notification
	}
}

func strict(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return errors.New("parameters are required")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func value(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func (s *Server) call(p callParams) map[string]any {
	method, path, body, err := routeTool(p.Name, p.Arguments)
	if err != nil {
		return toolError(err)
	}
	raw, err := s.Client.Do(method, path, body)
	if err != nil {
		return toolError(err)
	}
	var structured any
	if json.Unmarshal(raw, &structured) != nil {
		structured = map[string]any{"raw": string(raw)}
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(raw)}}, "structuredContent": structured, "isError": false}
}
func toolError(err error) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}
}

func routeTool(name string, a map[string]any) (string, string, any, error) {
	getString := func(key string, required bool) (string, error) {
		v, ok := a[key]
		if !ok {
			if required {
				return "", fmt.Errorf("%s is required", key)
			}
			return "", nil
		}
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("%s must be a non-empty string", key)
		}
		return s, nil
	}
	tx, _ := getString("transaction_id", false)
	switch name {
	case "futurediff.transaction_create":
		repo, err := getString("repository", true)
		if err != nil {
			return "", "", nil, err
		}
		mode, _ := getString("mode", false)
		if mode == "" {
			mode = "enforced"
		}
		policy, _ := getString("policy_version", false)
		if policy == "" {
			policy = "policy-0.1"
		}
		return "POST", "/v1/transactions", map[string]any{"repository": repo, "mode": mode, "policy_version": policy, "agent_adapter": "mcp"}, nil
	case "futurediff.transaction_status":
		if tx == "" {
			return "", "", nil, errors.New("transaction_id is required")
		}
		return "GET", "/v1/transactions/" + tx, nil, nil
	case "futurediff.transaction_execute":
		if tx == "" {
			return "", "", nil, errors.New("transaction_id is required")
		}
		raw, ok := a["command"].([]any)
		if !ok || len(raw) == 0 {
			return "", "", nil, errors.New("command must be a non-empty array")
		}
		cmd := make([]string, len(raw))
		for i, v := range raw {
			s, ok := v.(string)
			if !ok || s == "" {
				return "", "", nil, errors.New("command entries must be strings")
			}
			cmd[i] = s
		}
		body := map[string]any{"command": cmd}
		if env, ok := a["environment"].(map[string]any); ok {
			body["environment"] = env
		}
		return "POST", "/v1/transactions/" + tx + "/execute", body, nil
	case "futurediff.transaction_seal":
		if tx == "" {
			return "", "", nil, errors.New("transaction_id is required")
		}
		return "POST", "/v1/transactions/" + tx + "/seal", map[string]any{}, nil
	case "futurediff.transaction_verify":
		if tx == "" {
			return "", "", nil, errors.New("transaction_id is required")
		}
		c, ok := a["contract"].(map[string]any)
		if !ok {
			return "", "", nil, errors.New("contract object is required")
		}
		return "POST", "/v1/transactions/" + tx + "/verify", c, nil
	case "futurediff.effects_list":
		if tx == "" {
			return "", "", nil, errors.New("transaction_id is required")
		}
		return "GET", "/v1/transactions/" + tx + "/effects", nil, nil
	case "futurediff.github_branch_prepare":
		if tx == "" {
			return "", "", nil, errors.New("transaction_id is required")
		}
		cred, e := getString("credential_id", true)
		if e != nil {
			return "", "", nil, e
		}
		owner, e := getString("owner", true)
		if e != nil {
			return "", "", nil, e
		}
		repo, e := getString("repo", true)
		if e != nil {
			return "", "", nil, e
		}
		branch, e := getString("branch", true)
		if e != nil {
			return "", "", nil, e
		}
		remote, e := getString("remote_url", true)
		if e != nil {
			return "", "", nil, e
		}
		return "POST", "/v1/transactions/" + tx + "/effects/github/branch", map[string]any{"credential_id": cred, "owner": owner, "repo": repo, "branch": branch, "remote_url": remote}, nil
	case "futurediff.github_pr_prepare":
		if tx == "" {
			return "", "", nil, errors.New("transaction_id is required")
		}
		cred, e := getString("credential_id", true)
		if e != nil {
			return "", "", nil, e
		}
		owner, e := getString("owner", true)
		if e != nil {
			return "", "", nil, e
		}
		repo, e := getString("repo", true)
		if e != nil {
			return "", "", nil, e
		}
		base, e := getString("base", true)
		if e != nil {
			return "", "", nil, e
		}
		title, e := getString("title", true)
		if e != nil {
			return "", "", nil, e
		}
		input := map[string]any{"owner": owner, "repo": repo, "base": base, "title": title}
		for _, k := range []string{"body", "head", "depends_on_effect_id"} {
			if v, ok := a[k]; ok {
				input[k] = v
			}
		}
		return "POST", "/v1/transactions/" + tx + "/effects/github/draft-pull-request", map[string]any{"credential_id": cred, "input": input}, nil
	case "futurediff.slack_message_prepare":
		if tx == "" {
			return "", "", nil, errors.New("transaction_id is required")
		}
		cred, e := getString("credential_id", true)
		if e != nil {
			return "", "", nil, e
		}
		channel, e := getString("channel", true)
		if e != nil {
			return "", "", nil, e
		}
		text, e := getString("text", true)
		if e != nil {
			return "", "", nil, e
		}
		input := map[string]any{"channel": channel, "text": text}
		if deps, ok := a["depends_on"].([]any); ok {
			input["depends_on"] = deps
		}
		return "POST", "/v1/transactions/" + tx + "/effects/slack/message", map[string]any{"credential_id": cred, "input": input}, nil
	default:
		return "", "", nil, fmt.Errorf("unknown or forbidden tool %q", name)
	}
}

func tools() []tool {
	obj := func(required []string, props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	arr := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
	}
	read := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	stage := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false}
	return []tool{
		{Name: "futurediff.transaction_create", Title: "Create staged future", Description: "Create an isolated FutureDiff transaction. This does not approve or commit real effects.", InputSchema: obj([]string{"repository"}, map[string]any{"repository": str("Absolute Git repository path"), "mode": map[string]any{"type": "string", "enum": []string{"cooperative", "enforced"}}, "policy_version": str("Policy version")}), Annotations: stage},
		{Name: "futurediff.transaction_status", Title: "Inspect future", Description: "Read transaction, workspace, patch, effects, and receipts.", InputSchema: obj([]string{"transaction_id"}, map[string]any{"transaction_id": str("FutureDiff transaction ID")}), Annotations: read},
		{Name: "futurediff.transaction_execute", Title: "Execute in future", Description: "Run a command only inside an enforced staged workspace.", InputSchema: obj([]string{"transaction_id", "command"}, map[string]any{"transaction_id": str("Transaction ID"), "command": arr("Command and arguments"), "environment": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}}), Annotations: stage},
		{Name: "futurediff.transaction_seal", Title: "Seal future", Description: "Capture the exact staged patch and prevent further agent mutation.", InputSchema: obj([]string{"transaction_id"}, map[string]any{"transaction_id": str("Transaction ID")}), Annotations: stage},
		{Name: "futurediff.transaction_verify", Title: "Verify future", Description: "Run a deterministic verification contract. The model cannot override results.", InputSchema: obj([]string{"transaction_id", "contract"}, map[string]any{"transaction_id": str("Transaction ID"), "contract": map[string]any{"type": "object"}}), Annotations: stage},
		{Name: "futurediff.effects_list", Title: "List staged effects", Description: "List prepared external effects and dependencies.", InputSchema: obj([]string{"transaction_id"}, map[string]any{"transaction_id": str("Transaction ID")}), Annotations: read},
		{Name: "futurediff.github_branch_prepare", Title: "Prepare GitHub branch", Description: "Prepare create-only publication of the exact approved Git commit.", InputSchema: obj([]string{"transaction_id", "credential_id", "owner", "repo", "branch", "remote_url"}, map[string]any{"transaction_id": str("Transaction ID"), "credential_id": str("Broker credential handle"), "owner": str("GitHub owner"), "repo": str("GitHub repository"), "branch": str("futurediff/* branch"), "remote_url": str("Credential-free HTTPS Git remote")}), Annotations: stage},
		{Name: "futurediff.github_pr_prepare", Title: "Prepare draft PR", Description: "Prepare an exact GitHub draft pull request, optionally dependent on a branch effect.", InputSchema: obj([]string{"transaction_id", "credential_id", "owner", "repo", "base", "title"}, map[string]any{"transaction_id": str("Transaction ID"), "credential_id": str("Broker credential handle"), "owner": str("GitHub owner"), "repo": str("Repository"), "base": str("Base branch"), "title": str("PR title"), "body": str("PR body"), "head": str("Existing head branch"), "depends_on_effect_id": str("Prepared branch effect ID")}), Annotations: stage},
		{Name: "futurediff.slack_message_prepare", Title: "Prepare Slack outbox message", Description: "Prepare but do not send an exact Slack message.", InputSchema: obj([]string{"transaction_id", "credential_id", "channel", "text"}, map[string]any{"transaction_id": str("Transaction ID"), "credential_id": str("Broker credential handle"), "channel": str("Slack channel ID"), "text": str("Exact message text"), "depends_on": arr("Effect IDs that must commit first")}), Annotations: stage},
	}
}
