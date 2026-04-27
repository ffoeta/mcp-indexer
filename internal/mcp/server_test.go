package mcp

import (
	"context"
	"encoding/json"
	"mcp-indexer/internal/app"
	"mcp-indexer/internal/common/services"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ───────── helpers ─────────

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

func makeServer(t *testing.T) (*server.MCPServer, *app.App) {
	t.Helper()
	srv := server.NewMCPServer("test", "0.0.1")
	a := makeTestApp(t)
	Register(srv, a)
	return srv, a
}

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

func extractText(t *testing.T, r *mcpgo.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("empty content")
	}
	tc, ok := r.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	return tc.Text
}

func registerSimpleService(t *testing.T, a *app.App, svcID string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	id, err := a.AddService(root, svcID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// ───────── infra tests ─────────

func TestRegister_NoPanic(t *testing.T) {
	srv, _ := makeServer(t)
	_ = srv
}

func TestJsonResult_ProducesTextResult(t *testing.T) {
	result, err := jsonResult(map[string]string{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil")
	}
	tc, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("type=%T", result.Content[0])
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil || m["key"] != "value" {
		t.Errorf("bad result: %q (err=%v)", tc.Text, err)
	}
}

func TestErrResult_ProducesErrorResult(t *testing.T) {
	result := errResult(errMsg("boom"))
	if !result.IsError {
		t.Error("IsError must be true")
	}
	if !strings.Contains(extractText(t, result), "boom") {
		t.Errorf("error text missing")
	}
}

// ───────── services / add / update / sync ─────────

func TestServices_ReturnsList(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc1", map[string]string{"x.py": "class X: pass\n"})

	result := callTool(t, srv, "services", nil)
	if result.IsError {
		t.Fatal(extractText(t, result))
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(extractText(t, result)), &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0]["id"] != "svc1" {
		t.Errorf("bad services list: %+v", arr)
	}
}

func TestAddService_PersistsMeta(t *testing.T) {
	srv, a := makeServer(t)
	root := t.TempDir()

	result := callTool(t, srv, "add_service", map[string]interface{}{
		"rootAbs":      root,
		"serviceId":    "meta-test",
		"description":  "test service",
		"mainEntities": `["order","supplier"]`,
	})
	if result.IsError {
		t.Fatal(extractText(t, result))
	}
	entry, ok := a.Registry.Get("meta-test")
	if !ok {
		t.Fatal("not registered")
	}
	if entry.Description != "test service" || len(entry.MainEntities) != 2 {
		t.Errorf("meta not saved: %+v", entry)
	}
}

func TestUpdateMeta_BadJSON_ReturnsError(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc", map[string]string{"x.py": "x = 1\n"})

	result := callTool(t, srv, "update_service_meta", map[string]interface{}{
		"serviceId":    "svc",
		"mainEntities": "not-json",
	})
	if !result.IsError {
		t.Error("expected error for bad JSON")
	}
}

func TestSync_UnknownService_ReturnsError(t *testing.T) {
	srv, _ := makeServer(t)
	result := callTool(t, srv, "sync", map[string]interface{}{"serviceId": "ghost"})
	if !result.IsError {
		t.Error("expected error for ghost service")
	}
}

// ───────── stats ─────────

func TestStats_NonZero(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc", map[string]string{"x.py": "class Foo:\n    def m(self): pass\n"})

	result := callTool(t, srv, "stats", map[string]interface{}{"serviceId": "svc"})
	if result.IsError {
		t.Fatal(extractText(t, result))
	}
	var m map[string]int
	json.Unmarshal([]byte(extractText(t, result)), &m)
	if m["files"] == 0 || m["objects"] == 0 || m["methods"] == 0 {
		t.Errorf("stats zero: %+v", m)
	}
}

// ───────── search / peek / walk / file / code ─────────

func TestSearch_FindsObjectByShortID(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc", map[string]string{
		"svc.py": "class OrderService:\n    def run(self): pass\n",
	})

	result := callTool(t, srv, "search", map[string]interface{}{
		"serviceId": "svc",
		"query":     "order",
	})
	if result.IsError {
		t.Fatal(extractText(t, result))
	}
	var resp struct {
		Hits []struct {
			ID   string `json:"id"`
			K    string `json:"k"`
			Name string `json:"name"`
		} `json:"hits"`
	}
	json.Unmarshal([]byte(extractText(t, result)), &resp)
	if len(resp.Hits) == 0 {
		t.Fatal("no hits")
	}
	hasObj := false
	for _, h := range resp.Hits {
		if h.K == "object" && h.Name == "OrderService" {
			if !strings.HasPrefix(h.ID, "o") {
				t.Errorf("expected short id prefix 'o', got %q", h.ID)
			}
			hasObj = true
		}
	}
	if !hasObj {
		t.Errorf("OrderService not in hits: %+v", resp.Hits)
	}
}

func TestPeek_ObjectIncludesMethods(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc", map[string]string{
		"svc.py": "class Service:\n    def a(self): pass\n    def b(self): pass\n",
	})
	// Найдём short_id Service через search.
	res := callTool(t, srv, "search", map[string]interface{}{"serviceId": "svc", "query": "service", "kind": "object"})
	var search struct {
		Hits []struct{ ID string }
	}
	json.Unmarshal([]byte(extractText(t, res)), &search)
	if len(search.Hits) == 0 {
		t.Fatal("no service hit")
	}
	objID := search.Hits[0].ID

	pk := callTool(t, srv, "peek", map[string]interface{}{"serviceId": "svc", "id": objID})
	if pk.IsError {
		t.Fatal(extractText(t, pk))
	}
	var obj map[string]interface{}
	json.Unmarshal([]byte(extractText(t, pk)), &obj)
	if obj["k"] != "object" {
		t.Errorf("k=%v, want object", obj["k"])
	}
	methods, _ := obj["methods"].([]interface{})
	if len(methods) != 2 {
		t.Errorf("expected 2 methods, got %v", methods)
	}
}

func TestPeek_MethodCounts(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc", map[string]string{
		"x.py": `
def helper():
    pass

def main():
    helper()
`,
	})
	res := callTool(t, srv, "search", map[string]interface{}{"serviceId": "svc", "query": "helper", "kind": "method"})
	var search struct {
		Hits []struct{ ID, Name string }
	}
	json.Unmarshal([]byte(extractText(t, res)), &search)
	var helperID string
	for _, h := range search.Hits {
		if h.Name == "helper" {
			helperID = h.ID
			break
		}
	}
	if helperID == "" {
		t.Fatal("helper not found")
	}
	pk := callTool(t, srv, "peek", map[string]interface{}{"serviceId": "svc", "id": helperID})
	if pk.IsError {
		t.Fatal(extractText(t, pk))
	}
	var m map[string]interface{}
	json.Unmarshal([]byte(extractText(t, pk)), &m)
	if m["k"] != "method" || m["called_by"] == nil || int(m["called_by"].(float64)) < 1 {
		t.Errorf("helper.called_by missing or zero: %+v", m)
	}
}

func TestWalk_CallsIn(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc", map[string]string{
		"x.py": "def helper():\n    pass\ndef caller():\n    helper()\n",
	})
	res := callTool(t, srv, "search", map[string]interface{}{"serviceId": "svc", "query": "helper", "kind": "method"})
	var search struct {
		Hits []struct{ ID, Name string }
	}
	json.Unmarshal([]byte(extractText(t, res)), &search)
	var helperID string
	for _, h := range search.Hits {
		if h.Name == "helper" {
			helperID = h.ID
		}
	}
	w := callTool(t, srv, "walk", map[string]interface{}{
		"serviceId": "svc", "id": helperID, "edge": "calls", "dir": "in",
	})
	if w.IsError {
		t.Fatal(extractText(t, w))
	}
	var resp struct {
		Items []map[string]interface{}
		Total int
	}
	json.Unmarshal([]byte(extractText(t, w)), &resp)
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 in-call, got %+v", resp.Items)
	}
}

func TestFile_Overview(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc", map[string]string{
		"main.py": "import os\nclass A: pass\ndef f(): pass\n",
	})
	r := callTool(t, srv, "file", map[string]interface{}{"serviceId": "svc", "path": "main.py"})
	if r.IsError {
		t.Fatal(extractText(t, r))
	}
	var f map[string]interface{}
	json.Unmarshal([]byte(extractText(t, r)), &f)
	if f["lang"] != "python" || f["path"] != "main.py" {
		t.Errorf("bad file overview: %+v", f)
	}
	if objs, _ := f["objects"].([]interface{}); len(objs) != 1 {
		t.Errorf("expected 1 object, got %v", objs)
	}
}

func TestCode_ReadsRange(t *testing.T) {
	srv, a := makeServer(t)
	registerSimpleService(t, a, "svc", map[string]string{
		"x.py": "def hello():\n    return 42\n",
	})
	res := callTool(t, srv, "search", map[string]interface{}{"serviceId": "svc", "query": "hello", "kind": "method"})
	var search struct {
		Hits []struct{ ID, Name string }
	}
	json.Unmarshal([]byte(extractText(t, res)), &search)
	var helloID string
	for _, h := range search.Hits {
		if h.Name == "hello" {
			helloID = h.ID
		}
	}
	r := callTool(t, srv, "code", map[string]interface{}{"serviceId": "svc", "id": helloID})
	if r.IsError {
		t.Fatal(extractText(t, r))
	}
	var c map[string]interface{}
	json.Unmarshal([]byte(extractText(t, r)), &c)
	src, _ := c["src"].(string)
	if !strings.Contains(src, "return 42") {
		t.Errorf("src missing body: %q", src)
	}
}

// ───────── error paths ─────────

func TestPeek_UnknownService_ReturnsError(t *testing.T) {
	srv, _ := makeServer(t)
	result := callTool(t, srv, "peek", map[string]interface{}{
		"serviceId": "ghost", "id": "m1",
	})
	if !result.IsError {
		t.Error("expected error for unknown service")
	}
}

type errMsg string

func (e errMsg) Error() string { return string(e) }
