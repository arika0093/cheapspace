package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"cheapspace/internal/config"
	"cheapspace/internal/db"
	"cheapspace/internal/runtime"
	"cheapspace/internal/service"
)

func TestWorkspacesPageRenders(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/workspaces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Workspace inventory") {
		t.Fatalf("expected response body to contain workspace inventory heading")
	}
	if !strings.Contains(rec.Body.String(), "Include deleted workspaces") {
		t.Fatalf("expected response body to contain deleted workspace toggle")
	}
}

func TestCreateWorkspaceRedirects(t *testing.T) {
	t.Parallel()

	handler, cleanup := newTestHandler(t)
	defer cleanup()

	form := url.Values{
		"name":        {"handler-test"},
		"source_type": {"builtin_image"},
		"cpu_cores":   {"2"},
		"memory_mb":   {"1024"},
		"ttl_minutes": {"10"},
		"ssh_keys":    {""},
		"repo_url":    {"https://github.com/example/repo.git"},
		"repo_branch": {"main"},
		"ssh_port":    {"2301"},
		"http_proxy":  {"http://proxy.internal:8080"},
	}
	req := httptest.NewRequest(http.MethodPost, "/workspaces", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/workspaces/ws-") {
		t.Fatalf("expected workspace redirect, got %q", location)
	}
}

func newTestHandler(t *testing.T) (http.Handler, func()) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		Addr:                  "127.0.0.1:0",
		DataDir:               tempDir,
		DBPath:                filepath.Join(tempDir, "cheapspace.db"),
		Runtime:               "mock",
		PublicHost:            "localhost",
		DefaultWorkspaceImage: "ghcr.io/arika0093/cheapspace-workspace:test",
		AppSecret:             "test-secret",
		MaxCPUMillis:          8000,
		MaxMemoryMB:           16384,
		MaxTTLMinutes:         1440,
		PortRangeStart:        2300,
		PortRangeEnd:          2400,
		WorkspaceNetwork:      "bridge",
	}

	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("db.Migrate() error = %v", err)
	}

	store := db.NewStore(sqlDB)
	svc := service.New(cfg, store, runtime.NewMock())
	server, err := New(cfg, svc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return server.Routes(), func() {
		_ = store.Close()
	}
}
