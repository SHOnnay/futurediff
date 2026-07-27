package mcpbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeClient struct {
	method, path string
	body         any
	calls        int
}

func (f *fakeClient) Do(method, path string, body any) (json.RawMessage, error) {
	f.method, f.path, f.body = method, path, body
	f.calls++
	return json.RawMessage(`{"transaction":{"transaction_id":"tx_1","status":"active"}}`), nil
}

func TestStdioLifecycleToolsAndCall(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"futurediff.transaction_create","arguments":{"repository":"/tmp/repo","mode":"cooperative"}}}`,
	}, "\n") + "\n"
	client := &fakeClient{}
	server := &Server{Client: client, Name: "futurediff", Version: "test"}
	var out bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("responses=%d output=%s", len(lines), out.String())
	}
	var initialize map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &initialize); err != nil {
		t.Fatal(err)
	}
	result := initialize["result"].(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Fatalf("initialize=%#v", result)
	}
	var listed map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &listed)
	toolsResult := listed["result"].(map[string]any)
	toolList := toolsResult["tools"].([]any)
	for _, raw := range toolList {
		name := raw.(map[string]any)["name"].(string)
		if strings.Contains(name, "approve") || strings.HasSuffix(name, ".commit") {
			t.Fatalf("privileged tool leaked: %s", name)
		}
	}
	if client.calls != 1 || client.method != "POST" || client.path != "/v1/transactions" {
		t.Fatalf("call=%d %s %s", client.calls, client.method, client.path)
	}
}

func TestForbiddenToolReturnsToolErrorNotRPCFailure(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}` + "\n" + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"futurediff.commit","arguments":{"transaction_id":"tx"}}}` + "\n"
	var out bytes.Buffer
	s := &Server{Client: &fakeClient{}}
	if err := s.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var response map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &response)
	result := response["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("result=%#v", result)
	}
}

func TestCallsRequireInitialization(t *testing.T) {
	var out bytes.Buffer
	s := &Server{Client: &fakeClient{}}
	_ = s.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`+"\n"), &out)
	if !strings.Contains(out.String(), "Server is not initialized") {
		t.Fatalf("output=%s", out.String())
	}
}
