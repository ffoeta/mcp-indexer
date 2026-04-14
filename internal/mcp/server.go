package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp-indexer/internal/app"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Register регистрирует все MCP tools на сервере.
func Register(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("getServiceList",
			mcpgo.WithDescription("List all registered service IDs"),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return jsonResult(a.Registry.List())
		},
	)

	srv.AddTool(
		mcpgo.NewTool("addService",
			mcpgo.WithDescription("Register a new service for indexing"),
			mcpgo.WithString("rootAbs", mcpgo.Required(), mcpgo.Description("Absolute path to service root")),
			mcpgo.WithString("serviceId", mcpgo.Description("Optional service ID (derived from dir name if omitted)")),
			mcpgo.WithString("name", mcpgo.Description("Optional human-readable name")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			rootAbs := req.GetString("rootAbs", "")
			svcID := req.GetString("serviceId", "")
			name := req.GetString("name", "")
			id, err := a.AddService(rootAbs, svcID, name)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(map[string]string{"serviceId": id})
		},
	)

	srv.AddTool(
		mcpgo.NewTool("getServiceInfo",
			mcpgo.WithDescription("Get info about a registered service"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			info, err := a.GetServiceInfo(id)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(info)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("prepareSync",
			mcpgo.WithDescription("Stat-only diff: what would change (no writes)"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			res, err := a.PrepareSync(id)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("doSync",
			mcpgo.WithDescription("Hash diff + apply to index"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			res, err := a.DoSync(id)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("getProjectOverview",
			mcpgo.WithDescription("Summary counts: files, modules, symbols, edges"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			res, err := a.GetProjectOverview(id)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("search",
			mcpgo.WithDescription("Search symbols/files/modules by query"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("query", mcpgo.Required()),
			mcpgo.WithString("limits", mcpgo.Description(`JSON: {"sym":20,"file":10,"mod":5}. Set 0 to skip a type.`)),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			query := req.GetString("query", "")
			limits := app.DefaultSearchLimits()
			if limStr := req.GetString("limits", ""); limStr != "" {
				if err := json.Unmarshal([]byte(limStr), &limits); err != nil {
					return errResult(fmt.Errorf("invalid limits: %w", err)), nil
				}
			}
			res, err := a.Search(id, query, limits)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("getFileContext",
			mcpgo.WithDescription("File info: module, imports, symbols"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("File key (pathPrefix+relPath)")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			path := req.GetString("path", "")
			res, err := a.GetFileContext(id, path)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("getSymbolContext",
			mcpgo.WithDescription("Symbol info by symbolId"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("symbolId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			symID := req.GetString("symbolId", "")
			res, err := a.GetSymbolContext(id, symID)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("getNeighbors",
			mcpgo.WithDescription("BFS neighbors in the dependency graph"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("nodeId", mcpgo.Required()),
			mcpgo.WithNumber("depth", mcpgo.Description("BFS depth (default 2)")),
			mcpgo.WithString("edgeTypes", mcpgo.Description("Comma-separated edge types to filter (empty = all)")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			nodeID := req.GetString("nodeId", "")
			depth := req.GetInt("depth", 2)
			var edgeTypes []string
			if et := req.GetString("edgeTypes", ""); et != "" {
				edgeTypes = strings.Split(et, ",")
			}
			res, err := a.GetNeighbors(id, nodeID, depth, edgeTypes)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)
}

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
