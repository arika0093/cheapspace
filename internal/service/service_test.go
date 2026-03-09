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

func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		Addr:                  "127.0.0.1:0",
		DataDir:               tempDir,
		DBPath:                filepath.Join(tempDir, "cheapspace.db"),
		Runtime:               "mock",
		PublicHost:            "localhost",
		DefaultWorkspaceImage: "ghcr.io/cheapspace/workspace:test",
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
