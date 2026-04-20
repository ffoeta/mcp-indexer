package mcp

import (
	"context"
	"encoding/json"
	"mcp-indexer/internal/app"
	"mcp-indexer/internal/services"
	"os"
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

// N5: DebugConfigGet_ReturnsConfigPath
func TestDebugConfigGet_ReturnsConfigPath(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)
	home := services.AppHome()

	result := callTool(t, srv, "debug__config__get", nil)
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

// N6: SymbolFullGet_UnknownService_ReturnsError
func TestSymbolFullGet_UnknownService_ReturnsError(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)

	result := callTool(t, srv, "symbol__full__get", map[string]interface{}{
		"serviceId": "ghost",
		"symbolId":  "s:py:Foo:a.py:1",
	})
	if !result.IsError {
		t.Error("expected error result for unknown service")
	}
}

// N7: Help_ReturnsValidJSON
func TestHelp_ReturnsValidJSON(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)

	result := callTool(t, srv, "help", nil)
	if result.IsError {
		t.Fatalf("unexpected error from help: %v", result.Content)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(text.Text), &m); err != nil {
		t.Fatalf("help result is not valid JSON: %v", err)
	}
	if m["server"] == nil {
		t.Error("expected 'server' field in help result")
	}
	if m["tools"] == nil {
		t.Error("expected 'tools' field in help result")
	}
}

// N8: ServiceAdd_WithMeta_PersistsMeta
func TestServiceAdd_WithMeta_PersistsMeta(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)
	root := t.TempDir()

	result := callTool(t, srv, "service__add", map[string]interface{}{
		"rootAbs":      root,
		"serviceId":    "meta-test",
		"description":  "test service",
		"mainEntities": `["order","supplier"]`,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	entry, ok := a.Registry.Get("meta-test")
	if !ok {
		t.Fatal("service not registered")
	}
	if entry.Description != "test service" {
		t.Errorf("description not saved: %q", entry.Description)
	}
	if len(entry.MainEntities) != 2 || entry.MainEntities[0] != "order" {
		t.Errorf("mainEntities not saved: %v", entry.MainEntities)
	}
}

// N9: ServiceMetaUpdate_UpdatesFields
func TestServiceMetaUpdate_UpdatesFields(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)
	root := t.TempDir()

	// Register service first
	callTool(t, srv, "service__add", map[string]interface{}{
		"rootAbs":   root,
		"serviceId": "upd-test",
	})

	result := callTool(t, srv, "service__meta__update", map[string]interface{}{
		"serviceId":    "upd-test",
		"description":  "new description",
		"mainEntities": `["entity1"]`,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	entry, ok := a.Registry.Get("upd-test")
	if !ok {
		t.Fatal("service not found")
	}
	if entry.Description != "new description" {
		t.Errorf("description not updated: %q", entry.Description)
	}
}

// N10: ServiceMetaUpdate_InvalidMainEntities_ReturnsError
func TestServiceMetaUpdate_InvalidMainEntities_ReturnsError(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)
	root := t.TempDir()

	callTool(t, srv, "service__add", map[string]interface{}{
		"rootAbs":   root,
		"serviceId": "bad-entities",
	})

	result := callTool(t, srv, "service__meta__update", map[string]interface{}{
		"serviceId":    "bad-entities",
		"mainEntities": `not-json`,
	})
	if !result.IsError {
		t.Error("expected error for invalid mainEntities JSON")
	}
}

// N11: ServiceListGet_ReturnsFullEntries
func TestServiceListGet_ReturnsFullEntries(t *testing.T) {
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)
	root := t.TempDir()

	callTool(t, srv, "service__add", map[string]interface{}{
		"rootAbs":     root,
		"serviceId":   "list-test",
		"description": "list test service",
	})

	result := callTool(t, srv, "service__list__get", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(text.Text), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	entry, ok := m["list-test"].(map[string]interface{})
	if !ok {
		t.Fatal("list-test not in service list")
	}
	if entry["description"] != "list test service" {
		t.Errorf("description not returned in list: %v", entry)
	}
}

// makeTestAppWithService registers one service and returns app + svcID.
func makeTestAppWithService(t *testing.T) (*app.App, string) {
	t.Helper()
	a := makeTestApp(t)
	root := t.TempDir()
	os.WriteFile(root+"/foo.py", []byte("class Foo:\n    pass\n"), 0o644)
	svcID, err := a.AddService(root, "tsvc", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DoSync(svcID); err != nil {
		t.Fatal(err)
	}
	return a, svcID
}

type errMsg string

func (e errMsg) Error() string { return string(e) }
