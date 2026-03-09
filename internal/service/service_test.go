package service

import (
	"context"
	"path/filepath"
	"testing"

	"cheapspace/internal/config"
	"cheapspace/internal/db"
	"cheapspace/internal/runtime"
)

func TestWorkspaceLifecycle(t *testing.T) {
	t.Parallel()

	svc, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	workspace, err := svc.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name:                "lifecycle",
		RepoURL:             "https://github.com/example/repo.git",
		RepoBranch:          "main",
		SourceType:          "builtin_image",
		CPUMillis:           1000,
		MemoryMB:            1024,
		TTLMinutes:          15,
		PasswordAuthEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	if err := svc.RunUntilIdle(ctx, 12); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}

	details, err := svc.GetWorkspaceDetails(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceDetails() error = %v", err)
	}
	if details.Workspace.State != "running" {
		t.Fatalf("expected workspace to be running, got %q", details.Workspace.State)
	}
	if details.Workspace.RepoBranch != "main" {
		t.Fatalf("expected repo branch to persist, got %q", details.Workspace.RepoBranch)
	}
	if details.Workspace.SSHPort == 0 {
		t.Fatalf("expected SSH port to be assigned")
	}

	password, err := svc.RevealPassword(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("RevealPassword() error = %v", err)
	}
	if password == "" {
		t.Fatalf("expected password to be returned")
	}
	if _, err := svc.RevealPassword(ctx, workspace.ID); err == nil {
		t.Fatalf("expected second RevealPassword() to fail")
	}

	if err := svc.QueueWorkspaceDelete(ctx, workspace.ID); err != nil {
		t.Fatalf("QueueWorkspaceDelete() error = %v", err)
	}
	if err := svc.RunUntilIdle(ctx, 12); err != nil {
		t.Fatalf("RunUntilIdle(delete) error = %v", err)
	}

	details, err = svc.GetWorkspaceDetails(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceDetails() after delete error = %v", err)
	}
	if details.Workspace.State != "deleted" {
		t.Fatalf("expected workspace to be deleted, got %q", details.Workspace.State)
	}
}

func TestCancelAndResumeBuildJob(t *testing.T) {
	t.Parallel()

	svc, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	workspace, err := svc.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name:           "cancel-build",
		SourceType:     "dockerfile",
		SourceRef:      "FROM alpine:3.20\nRUN echo ready",
		CPUMillis:      1000,
		MemoryMB:       1024,
		TTLMinutes:     15,
		TraefikEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	jobs, err := svc.ListJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(jobs) == 0 {
		t.Fatalf("expected a queued job")
	}

	if err := svc.RequestJobCancel(ctx, jobs[0].ID); err != nil {
		t.Fatalf("RequestJobCancel() error = %v", err)
	}
	if err := svc.RunUntilIdle(ctx, 12); err != nil {
		t.Fatalf("RunUntilIdle(cancel) error = %v", err)
	}

	cancelled, err := svc.GetJobDetails(ctx, jobs[0].ID, 0)
	if err != nil {
		t.Fatalf("GetJobDetails() error = %v", err)
	}
	if cancelled.Job.Status != "cancelled" {
		t.Fatalf("expected job to be cancelled, got %q", cancelled.Job.Status)
	}

	if err := svc.ResumeJob(ctx, jobs[0].ID); err != nil {
		t.Fatalf("ResumeJob() error = %v", err)
	}
	if err := svc.RunUntilIdle(ctx, 20); err != nil {
		t.Fatalf("RunUntilIdle(resume) error = %v", err)
	}

	details, err := svc.GetWorkspaceDetails(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceDetails() error = %v", err)
	}
	if details.Workspace.State != "running" {
		t.Fatalf("expected resumed workspace to be running, got %q", details.Workspace.State)
	}
}

func TestWorkspacePasswordFallbackTracksSSHKeys(t *testing.T) {
	t.Parallel()

	svc, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	workspace, err := svc.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name:                "with-key",
		SourceType:          "builtin_image",
		SSHKeys:             []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICheapspaceTestKey user@example"},
		PasswordAuthEnabled: true,
		CPUMillis:           1000,
		MemoryMB:            1024,
		TTLMinutes:          15,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	if err := svc.RunUntilIdle(ctx, 12); err != nil {
		t.Fatalf("RunUntilIdle() error = %v", err)
	}

	details, err := svc.GetWorkspaceDetails(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceDetails() error = %v", err)
	}
	if details.Workspace.PasswordAuthEnabled {
		t.Fatalf("expected password fallback to be disabled when SSH keys are present")
	}
	if len(details.Keys) != 1 {
		t.Fatalf("expected one SSH key to be stored, got %d", len(details.Keys))
	}
	if _, err := svc.RevealPassword(ctx, workspace.ID); err == nil {
		t.Fatalf("expected RevealPassword() to fail when no password was generated")
	}
}

func TestRequestedPortReusedAndProxyMirrors(t *testing.T) {
	t.Parallel()

	svc, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	first, err := svc.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name:        "first-port",
		SourceType:  "builtin_image",
		SSHPort:     2305,
		HTTPProxy:   "http://proxy.internal:8080",
		ProxyPACURL: "https://proxy.internal/proxy.pac",
		CPUMillis:   1000,
		MemoryMB:    1024,
		TTLMinutes:  15,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(first) error = %v", err)
	}
	if err := svc.RunUntilIdle(ctx, 12); err != nil {
		t.Fatalf("RunUntilIdle(first) error = %v", err)
	}

	firstDetails, err := svc.GetWorkspaceDetails(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceDetails(first) error = %v", err)
	}
	if firstDetails.Workspace.SSHPort != 2305 {
		t.Fatalf("expected requested SSH port to be used, got %d", firstDetails.Workspace.SSHPort)
	}
	if firstDetails.Workspace.HTTPSProxy != firstDetails.Workspace.HTTPProxy {
		t.Fatalf("expected HTTPS proxy to mirror HTTP proxy, got %q and %q", firstDetails.Workspace.HTTPProxy, firstDetails.Workspace.HTTPSProxy)
	}
	if firstDetails.Workspace.ProxyPACURL != "https://proxy.internal/proxy.pac" {
		t.Fatalf("expected proxy PAC URL to persist, got %q", firstDetails.Workspace.ProxyPACURL)
	}

	if err := svc.QueueWorkspaceDelete(ctx, first.ID); err != nil {
		t.Fatalf("QueueWorkspaceDelete(first) error = %v", err)
	}
	if err := svc.RunUntilIdle(ctx, 12); err != nil {
		t.Fatalf("RunUntilIdle(delete first) error = %v", err)
	}

	visible, err := svc.ListWorkspaces(ctx, false)
	if err != nil {
		t.Fatalf("ListWorkspaces(false) error = %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("expected deleted workspaces to be hidden by default, got %d", len(visible))
	}
	all, err := svc.ListWorkspaces(ctx, true)
	if err != nil {
		t.Fatalf("ListWorkspaces(true) error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected deleted workspaces to be included when requested, got %d", len(all))
	}

	second, err := svc.CreateWorkspace(ctx, CreateWorkspaceInput{
		Name:       "second-port",
		SourceType: "builtin_image",
		SSHPort:    2305,
		CPUMillis:  1000,
		MemoryMB:   1024,
		TTLMinutes: 15,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(second) error = %v", err)
	}
	if err := svc.RunUntilIdle(ctx, 12); err != nil {
		t.Fatalf("RunUntilIdle(second) error = %v", err)
	}

	secondDetails, err := svc.GetWorkspaceDetails(ctx, second.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceDetails(second) error = %v", err)
	}
	if secondDetails.Workspace.SSHPort != 2305 {
		t.Fatalf("expected released port to be reusable, got %d", secondDetails.Workspace.SSHPort)
	}
}

func newTestService(t *testing.T) (*Service, func()) {
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
	svc := New(cfg, store, runtime.NewMock())
	cleanup := func() {
		_ = store.Close()
	}
	return svc, cleanup
}
