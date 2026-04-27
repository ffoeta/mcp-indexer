// Package mcp — тонкий MCP-слой: парсинг параметров → вызов App → JSON-ответ.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp-indexer/internal/app"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Register регистрирует все MCP tools на сервере.
//
// Поверхность из 10 инструментов:
//   - services / add_service / update_service_meta / sync — service mgmt
//   - stats — счётчики индекса
//   - search / peek / walk / code / file — основной graph navigation
func Register(srv *server.MCPServer, a *app.App) {
	registerServices(srv, a)
	registerAddService(srv, a)
	registerUpdateMeta(srv, a)
	registerSync(srv, a)
	registerStats(srv, a)
	registerSearch(srv, a)
	registerPeek(srv, a)
	registerWalk(srv, a)
	registerCode(srv, a)
	registerFile(srv, a)
}

// ───────── service mgmt ─────────

func registerServices(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("services",
			mcpgo.WithDescription("List registered services with id, root, description and main entities."),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return jsonResult(a.ListServices())
		},
	)
}

func registerAddService(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("add_service",
			mcpgo.WithDescription("Register a new service (codebase root) and run a full index. Returns the assigned serviceId."),
			mcpgo.WithString("rootAbs", mcpgo.Required(), mcpgo.Description("Absolute path to service root")),
			mcpgo.WithString("serviceId", mcpgo.Description("Optional service ID; derived from dir name if omitted")),
			mcpgo.WithString("description", mcpgo.Description("Optional short description")),
			mcpgo.WithString("mainEntities", mcpgo.Description(`Optional JSON array, e.g. ["supplier","order"]`)),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			rootAbs := req.GetString("rootAbs", "")
			svcID := req.GetString("serviceId", "")
			description := req.GetString("description", "")
			var mainEntities []string
			if me := req.GetString("mainEntities", ""); me != "" {
				if err := json.Unmarshal([]byte(me), &mainEntities); err != nil {
					return errResult(fmt.Errorf("invalid mainEntities: %w", err)), nil
				}
			}
			id, err := a.AddService(rootAbs, svcID, description, mainEntities)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(map[string]string{"serviceId": id})
		},
	)
}

func registerUpdateMeta(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("update_service_meta",
			mcpgo.WithDescription("Update description and/or main entities of a service. Empty values are kept."),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("description", mcpgo.Description("Omit to keep existing")),
			mcpgo.WithString("mainEntities", mcpgo.Description(`JSON array; omit to keep existing`)),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			description := req.GetString("description", "")
			var mainEntities []string
			if me := req.GetString("mainEntities", ""); me != "" {
				if err := json.Unmarshal([]byte(me), &mainEntities); err != nil {
					return errResult(fmt.Errorf("invalid mainEntities: %w", err)), nil
				}
			}
			if err := a.UpdateServiceMeta(id, description, mainEntities); err != nil {
				return errResult(err), nil
			}
			return jsonResult(map[string]string{"status": "ok"})
		},
	)
}

func registerSync(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("sync",
			mcpgo.WithDescription("Full re-index a service from scratch. Call after code changes."),
			mcpgo.WithString("serviceId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			if err := a.Sync(id); err != nil {
				return errResult(err), nil
			}
			return jsonResult(map[string]string{"status": "ok", "serviceId": id})
		},
	)
}

// ───────── stats ─────────

func registerStats(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("stats",
			mcpgo.WithDescription("Index counters: files, objects, methods, calls (resolved/unresolved), inherits, imports, fts docs."),
			mcpgo.WithString("serviceId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			res, err := a.Stats(id)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)
}

// ───────── search / peek / walk / code / file ─────────

func registerSearch(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("search",
			mcpgo.WithDescription("Find methods, objects or files by keyword (FTS5). Returns short ids + name/fqn/file/line. No code, no edges."),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Free-text query; tokenized server-side")),
			mcpgo.WithString("kind", mcpgo.Description(`"method" | "object" | "file"; empty = all`)),
			mcpgo.WithNumber("limit", mcpgo.Description("Default 10")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			q := req.GetString("query", "")
			kind := req.GetString("kind", "")
			limit := req.GetInt("limit", 10)
			res, err := a.Search(id, q, kind, limit)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)
}

func registerPeek(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("peek",
			mcpgo.WithDescription("Compact summary of a node (method/object) or file by short id ('m412'/'o7'/'f3') or canonical id. No source code (use code()), counts instead of arrays for callers/callees."),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Short id (m412/o7/f3) or canonical (n:.../f:...)")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			svc := req.GetString("serviceId", "")
			id := req.GetString("id", "")
			res, err := a.Peek(svc, id)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)
}

func registerWalk(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("walk",
			mcpgo.WithDescription("List edges around a node/file. edge: calls|inherits|imports|defines. dir: in|out|both. Unresolved edges have no `to`/`from` but include `hint`."),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
			mcpgo.WithString("edge", mcpgo.Required(), mcpgo.Description("calls | inherits | imports | defines")),
			mcpgo.WithString("dir", mcpgo.Description("in | out | both (default both)")),
			mcpgo.WithNumber("limit", mcpgo.Description("Default 20")),
			mcpgo.WithNumber("offset", mcpgo.Description("Default 0")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			svc := req.GetString("serviceId", "")
			id := req.GetString("id", "")
			edge := req.GetString("edge", "")
			dir := req.GetString("dir", "both")
			limit := req.GetInt("limit", 20)
			offset := req.GetInt("offset", 0)
			res, err := a.Walk(svc, id, edge, dir, limit, offset)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)
}

func registerCode(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("code",
			mcpgo.WithDescription("Read source code of a node (method/object) by short or canonical id. ctx adds extra lines around the range."),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
			mcpgo.WithNumber("ctx", mcpgo.Description("Lines of extra context before/after (default 0)")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			svc := req.GetString("serviceId", "")
			id := req.GetString("id", "")
			extra := req.GetInt("ctx", 0)
			res, err := a.Code(svc, id, extra)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)
}

func registerFile(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("file",
			mcpgo.WithDescription("File overview: imports, objects, top-level methods. No source code."),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("rel_path (e.g. pkg/svc.py) or canonical fileId")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			svc := req.GetString("serviceId", "")
			path := req.GetString("path", "")
			res, err := a.File(svc, path)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)
}

// ───────── helpers ─────────

func jsonResult(v interface{}) (*mcpgo.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mcpgo.NewToolResultText(string(data)), nil
}

func errResult(err error) *mcpgo.CallToolResult {
	return mcpgo.NewToolResultError(err.Error())
}