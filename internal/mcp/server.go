package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp-indexer/internal/app"
	"mcp-indexer/internal/common/services"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Register регистрирует все MCP tools на сервере.
func Register(srv *server.MCPServer, a *app.App) {
	srv.AddTool(
		mcpgo.NewTool("help",
			mcpgo.WithDescription("Description of this MCP server and all available tools"),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return jsonResult(helpPayload())
		},
	)

	srv.AddTool(
		mcpgo.NewTool("debug_get_config",
			mcpgo.WithDescription("[Debug] General info about the mcp-indexer instance (config path, home dir). Not needed in normal workflows."),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return jsonResult(map[string]string{
				"configPath": services.AppHome(),
			})
		},
	)

	srv.AddTool(
		mcpgo.NewTool("get_service_list",
			mcpgo.WithDescription("List all registered services: id → {name, rootAbs}"),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			full := a.Registry.ListFull()
			out := make(map[string]string, len(full))
			for id, e := range full {
				out[id] = e.RootAbs
			}
			return jsonResult(out)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("add_service",
			mcpgo.WithDescription("Register a new service for indexing"),
			mcpgo.WithString("rootAbs", mcpgo.Required(), mcpgo.Description("Absolute path to service root")),
			mcpgo.WithString("serviceId", mcpgo.Description("Optional service ID (derived from dir name if omitted)")),
			mcpgo.WithString("description", mcpgo.Description("Optional short description of the service")),
			mcpgo.WithString("mainEntities", mcpgo.Description(`Optional JSON array of main domain entities, e.g. ["supplier","order"]`)),
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

	srv.AddTool(
		mcpgo.NewTool("get_service_meta",
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
		mcpgo.NewTool("update_service_meta",
			mcpgo.WithDescription("Update description and/or mainEntities of a registered service"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("description", mcpgo.Description("New description (omit to keep existing)")),
			mcpgo.WithString("mainEntities", mcpgo.Description(`New main entities as JSON array, e.g. ["supplier","order"] (omit to keep existing)`)),
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

	srv.AddTool(
		mcpgo.NewTool("sync",
			mcpgo.WithDescription("Re-index a service from scratch (wipe + full re-index)"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			if err := a.Reindex(id); err != nil {
				return errResult(err), nil
			}
			return jsonResult(map[string]string{"status": "ok", "serviceId": id})
		},
	)

	srv.AddTool(
		mcpgo.NewTool("debug_get_project_stats",
			mcpgo.WithDescription("Index stats: total counts of indexed files, modules, symbols, and edges"),
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
		mcpgo.NewTool("debug_get_project_config",
			mcpgo.WithDescription("Service indexing config: pathPrefix, includeExt, ignoreFile, search.stopWords"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			res, err := a.GetServiceConfig(id)
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
			mcpgo.WithString("limits", mcpgo.Description(`JSON: {"sym":20,"file":10}. Set 0 to skip a type.`)),
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
		mcpgo.NewTool("get_file_context",
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
		mcpgo.NewTool("get_symbol_context",
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
		mcpgo.NewTool("get_symbol_full",
			mcpgo.WithDescription("Symbol metadata + source code + callers + graph edges in one call"),
			mcpgo.WithString("serviceId", mcpgo.Required()),
			mcpgo.WithString("symbolId", mcpgo.Required()),
			mcpgo.WithNumber("edgeDepth", mcpgo.Description("BFS depth for edges (default 1)")),
		),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			id := req.GetString("serviceId", "")
			symID := req.GetString("symbolId", "")
			depth := req.GetInt("edgeDepth", 1)
			res, err := a.GetSymbolFull(id, symID, depth)
			if err != nil {
				return errResult(err), nil
			}
			return jsonResult(res)
		},
	)

	srv.AddTool(
		mcpgo.NewTool("get_neighbors",
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

// helpPayload возвращает описание сервера и всех инструментов.
func helpPayload() map[string]interface{} {
	type toolDoc struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Params      []string `json:"params,omitempty"`
	}
	return map[string]interface{}{
		"server":      "mcp-indexer",
		"description": "MCP server for source code indexing. Scans Python and Java codebases, builds a SQLite index of files, modules, symbols, and dependency edges. Designed for LLM agents that need to navigate and understand large codebases.",
		"workflow": []string{
			"1. add_service       — register a codebase root (indexes automatically on first add)",
			"2. get_service_list  — see all registered services",
			"3. sync              — re-index a service from scratch when code changes",
			"4. search            — find symbols/files by keyword",
			"5. get_file_context / get_symbol_context / get_symbol_full — drill down",
			"6. get_neighbors     — traverse the dependency graph",
		},
		"tools": []toolDoc{
			{
				Name:        "add_service",
				Description: "Register a new service (codebase root) for indexing.",
				Params:      []string{"rootAbs (required)", "serviceId?", "description?", "mainEntities? (JSON array)"},
			},
			{
				Name:        "get_service_list",
				Description: "List all registered services: id → {rootAbs}. Lightweight overview — use get_service_meta for description, mainEntities.",
			},
			{
				Name:        "get_service_meta",
				Description: "Full info about one service: serviceId, rootAbs, description, mainEntities.",
				Params:      []string{"serviceId (required)"},
			},
			{
				Name:        "update_service_meta",
				Description: "Update description and/or mainEntities of an existing service. Call this after exploring or syncing a service to document what it does. Non-empty values overwrite; omitted values are kept.",
				Params:      []string{"serviceId (required)", "description?", "mainEntities? (JSON array)"},
			},
			{
				Name:        "sync",
				Description: "Re-index a service from scratch. Call this when the codebase has changed and you need fresh index data.",
				Params:      []string{"serviceId (required)"},
			},
			{
				Name:        "debug_get_project_stats",
				Description: "Index stats: total counts of indexed files, modules, symbols, and edges.",
				Params:      []string{"serviceId (required)"},
			},
			{
				Name:        "debug_get_project_config",
				Description: "Service indexing config: pathPrefix, includeExt, ignoreFile, search.stopWords.",
				Params:      []string{"serviceId (required)"},
			},
			{
				Name:        "search",
				Description: "Full-text search across symbols, files, and modules by keyword(s).",
				Params:      []string{"serviceId (required)", "query (required)", `limits? (JSON: {"sym":20,"file":10})`},
			},
			{
				Name:        "get_file_context",
				Description: "File details: owning module, all imports, all defined symbols.",
				Params:      []string{"serviceId (required)", "path (required) — file key, e.g. src:pkg/collector.py"},
			},
			{
				Name:        "get_symbol_context",
				Description: "Symbol metadata + source code snippet.",
				Params:      []string{"serviceId (required)", "symbolId (required)"},
			},
			{
				Name:        "get_symbol_full",
				Description: "Symbol metadata + source code + callers + graph edges in one call.",
				Params:      []string{"serviceId (required)", "symbolId (required)", "edgeDepth? (default 1)"},
			},
			{
				Name:        "get_neighbors",
				Description: "BFS traversal of the dependency graph from any node (file, module, symbol).",
				Params:      []string{"serviceId (required)", "nodeId (required)", "depth? (default 2)", "edgeTypes? (CSV: contains,imports,defines,calls,base)"},
			},
			{
				Name:        "debug_get_config",
				Description: "[Debug only] Returns config home path. Not needed in normal agent workflows.",
			},
		},
		"notes": []string{
			"After syncing or exploring a service, consider calling update_service_meta to document its purpose and key domain entities — this helps future sessions get up to speed faster.",
			"Symbol IDs have the format s:{lang}:{qualified}:{fileKey}:{startLine}. Obtain them via search or get_file_context.",
			"File keys have the format {pathPrefix}{relPath}, e.g. src:pkg/collector.py. pathPrefix is configured per service.",
		},
	}
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
