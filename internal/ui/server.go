// Package ui — HTTP-сервер для интерактивной визуализации индекса.
//
// Два режима в браузере:
//   1. Tree   — дерево файлов сервиса → overview файла → peek нод → код.
//   2. Pipeline — пайплайн агента: search → peek → walk соседей → drill.
//
// Все эндпоинты тонкие: парсят query → дёргают app.* → отдают JSON.
package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mcp-indexer/internal/app"
	"net/http"
	"strconv"
	"time"
)

//go:embed static/*
var staticFS embed.FS

// Serve запускает HTTP-сервер визуализации для конкретного сервиса.
func Serve(a *app.App, svcID string, port int) error {
	if _, ok := a.Registry.Get(svcID); !ok {
		return fmt.Errorf("service %q not found", svcID)
	}
	mux := http.NewServeMux()
	h := &handler{app: a, svcID: svcID}
	h.register(mux)

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           withLog(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("ui: serving %s on http://localhost:%d", svcID, port)
	return srv.ListenAndServe()
}

type handler struct {
	app   *app.App
	svcID string
}

func (h *handler) register(mux *http.ServeMux) {
	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("/api/info", h.info)
	mux.HandleFunc("/api/tree", h.tree)
	mux.HandleFunc("/api/file", h.file)
	mux.HandleFunc("/api/search", h.search)
	mux.HandleFunc("/api/peek", h.peek)
	mux.HandleFunc("/api/walk", h.walk)
	mux.HandleFunc("/api/code", h.code)
}

// ───────── handlers ─────────

func (h *handler) info(w http.ResponseWriter, _ *http.Request) {
	svc, err := h.app.GetServiceInfo(h.svcID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	stats, err := h.app.Stats(h.svcID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"service": svc,
		"stats":   stats,
	})
}

func (h *handler) tree(w http.ResponseWriter, _ *http.Request) {
	res, err := h.app.Tree(h.svcID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, res)
}

func (h *handler) file(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("path required"))
		return
	}
	res, err := h.app.File(h.svcID, path)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, res)
}

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("q")
	if query == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("q required"))
		return
	}
	kind := q.Get("kind")
	limit := atoiDefault(q.Get("limit"), 20)
	res, err := h.app.Search(h.svcID, query, kind, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, res)
}

func (h *handler) peek(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("id required"))
		return
	}
	res, err := h.app.Peek(h.svcID, id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, res)
}

func (h *handler) walk(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("id")
	edge := q.Get("edge")
	if id == "" || edge == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("id and edge required"))
		return
	}
	dir := q.Get("dir")
	if dir == "" {
		dir = "both"
	}
	limit := atoiDefault(q.Get("limit"), 50)
	offset := atoiDefault(q.Get("offset"), 0)
	res, err := h.app.Walk(h.svcID, id, edge, dir, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, res)
}

func (h *handler) code(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("id required"))
		return
	}
	ctx := atoiDefault(q.Get("ctx"), 0)
	res, err := h.app.Code(h.svcID, id, ctx)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, res)
}

// ───────── helpers ─────────

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("ui: encode error: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("ui: %s %s (%s)", r.Method, r.URL.Path, time.Since(t))
	})
}