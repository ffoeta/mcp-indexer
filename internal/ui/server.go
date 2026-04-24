package ui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"mcp-indexer/internal/app"
	"mcp-indexer/internal/searcher/query"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

//go:embed static/index.html
var staticFS embed.FS

// GraphData — ответ для D3.js форсированного графа.
type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Links []GraphLink `json:"links"`
}

type GraphNode struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

type GraphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// Serve запускает HTTP-сервер на указанном порту и открывает браузер.
func Serve(a *app.App, svcID string, port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		edges, err := a.GetAllEdges(svcID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, edgesToGraph(edges))
	})

	mux.HandleFunc("/api/neighbors", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node")
		if nodeID == "" {
			http.Error(w, "missing node param", http.StatusBadRequest)
			return
		}
		depth := 2
		if d, err := strconv.Atoi(r.URL.Query().Get("depth")); err == nil && d > 0 {
			depth = d
		}
		raw, err := a.GetNeighbors(svcID, nodeID, depth, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		edges, ok := raw.([]query.NeighborEdge)
		if !ok {
			http.Error(w, "unexpected result type", http.StatusInternalServerError)
			return
		}
		writeJSON(w, edgesToGraph(edges))
	})

	addr := fmt.Sprintf(":%d", port)
	url := fmt.Sprintf("http://localhost%s", addr)
	log.Printf("viz: %s  (service: %s)", url, svcID)

	// открыть браузер через небольшую задержку чтобы сервер успел стартовать
	go func() {
		time.Sleep(300 * time.Millisecond)
		openBrowser(url)
	}()

	srv := &http.Server{Addr: addr, Handler: mux}
	// graceful shutdown при отмене контекста не нужен — Ctrl+C завершает процесс
	_ = context.Background()
	return srv.ListenAndServe()
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "")
	if err := enc.Encode(v); err != nil {
		log.Printf("viz: json encode error: %v", err)
	}
}

// edgesToGraph конвертирует срез рёбер в формат {nodes, links} для D3.js.
func edgesToGraph(edges []query.NeighborEdge) GraphData {
	nodeSet := make(map[string]struct{})
	links := make([]GraphLink, 0, len(edges))

	for _, e := range edges {
		typ, from, to := e[0], e[1], e[2]
		nodeSet[from] = struct{}{}
		nodeSet[to] = struct{}{}
		links = append(links, GraphLink{Source: from, Target: to, Type: typ})
	}

	nodes := make([]GraphNode, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, GraphNode{ID: id, Type: nodeTypeOf(id), Label: nodeLabelOf(id)})
	}
	return GraphData{Nodes: nodes, Links: links}
}

func nodeTypeOf(id string) string {
	switch {
	case strings.HasPrefix(id, "f:"):
		return "file"
	case strings.HasPrefix(id, "m:"):
		return "module"
	case strings.HasPrefix(id, "s:"):
		return "symbol"
	default:
		return "unresolved"
	}
}

func nodeLabelOf(id string) string {
	switch {
	case strings.HasPrefix(id, "f:"):
		path := id[2:]
		if i := strings.LastIndex(path, "/"); i >= 0 {
			return path[i+1:]
		}
		return path
	case strings.HasPrefix(id, "m:"):
		// m:py:pkg.sub → pkg.sub
		parts := strings.SplitN(id, ":", 3)
		if len(parts) == 3 {
			return parts[2]
		}
		return id
	case strings.HasPrefix(id, "s:"):
		// s:py:ClassName:file.py:10 → ClassName
		parts := strings.SplitN(id, ":", 4)
		if len(parts) >= 3 {
			return parts[2]
		}
		return id
	case strings.HasPrefix(id, "x:"):
		return id[2:]
	default:
		return id
	}
}

func openBrowser(url string) {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "cmd"
	default:
		cmd = "xdg-open"
	}
	var err error
	if runtime.GOOS == "windows" {
		err = exec.Command(cmd, "/c", "start", url).Start()
	} else {
		err = exec.Command(cmd, url).Start()
	}
	if err != nil {
		log.Printf("viz: could not open browser: %v", err)
	}
}
