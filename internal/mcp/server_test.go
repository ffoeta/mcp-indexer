package mcp

import (
	"context"
	"encoding/json"
	"mcp-indexer/internal/app"
	"mcp-indexer/internal/services"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func makeTestApp(t *testing.T) *app.App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MCP_INDEXER_HOME", home)
	reg, err := services.LoadRegistry(services.RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	return app.NewFromRegistry(reg)
}

// N1: Register_NoPanic
func TestRegister_NoPanic(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a) // must not panic
}

// N2: jsonResult_ProducesTextResult
func TestJsonResult_ProducesTextResult(t *testing.T) {
	result, err := jsonResult(map[string]string{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(text.Text), &m); err != nil {
		t.Fatalf("result not valid JSON: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("unexpected content: %q", text.Text)
	}
}

// N3: errResult_ProducesErrorResult
func TestErrResult_ProducesErrorResult(t *testing.T) {
	result := errResult(errMsg("something went wrong"))
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in error result")
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent in error, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "something went wrong") {
		t.Errorf("error message not in result: %q", text.Text)
	}
}

// N4: jsonResult_List_IsValidJSON
func TestJsonResult_List_IsValidJSON(t *testing.T) {
	result, err := jsonResult([]string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	var list []string
	if err := json.Unmarshal([]byte(text.Text), &list); err != nil {
		t.Fatalf("result not valid JSON array: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 items, got %d", len(list))
	}
}

// callTool invokes a registered tool via HandleMessage and returns the CallToolResult.
// Fails the test on RPC-level errors (tool not found, protocol errors).
func callTool(t *testing.T, srv *server.MCPServer, name string, args map[string]interface{}) *mcpgo.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := srv.HandleMessage(context.Background(), raw)
	switch v := msg.(type) {
	case mcpgo.JSONRPCResponse:
		result, ok := v.Result.(mcpgo.CallToolResult)
		if !ok {
			t.Fatalf("unexpected result type: %T", v.Result)
		}
		return &result
	case mcpgo.JSONRPCError:
		t.Fatalf("RPC error %d: %s", v.Error.Code, v.Error.Message)
	default:
		t.Fatalf("unexpected message type: %T", msg)
	}
	return nil
}

// N5: GetInfo_ReturnsConfigPath
func TestGetInfo_ReturnsConfigPath(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)
	home := services.AppHome()

	result := callTool(t, srv, "getInfo", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(text.Text), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["configPath"] != home {
		t.Errorf("expected configPath=%q, got %q", home, m["configPath"])
	}
}

// N6: GetSymbolFull_UnknownService_ReturnsError
func TestGetSymbolFull_UnknownService_ReturnsError(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)

	result := callTool(t, srv, "getSymbolFull", map[string]interface{}{
		"serviceId": "ghost",
		"symbolId":  "s:py:Foo:a.py:1",
	})
	if !result.IsError {
		t.Error("expected error result for unknown service")
	}
}

type errMsg string

func (e errMsg) Error() string { return string(e) }
