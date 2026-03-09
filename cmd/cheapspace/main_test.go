package main

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWorkspaceCLIWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("CHEAPSPACE_DATA_DIR", tempDir)
	t.Setenv("CHEAPSPACE_DB_PATH", filepath.Join(tempDir, "cheapspace.db"))
	t.Setenv("CHEAPSPACE_RUNTIME", "mock")
	t.Setenv("CHEAPSPACE_PUBLIC_HOST", "localhost")
	t.Setenv("CHEAPSPACE_DEFAULT_WORKSPACE_IMAGE", "ghcr.io/arika0093/cheapspace-workspace:test")
	t.Setenv("CHEAPSPACE_APP_SECRET", "test-secret")
	t.Setenv("CHEAPSPACE_PORT_RANGE_START", "2300")
	t.Setenv("CHEAPSPACE_PORT_RANGE_END", "2400")
	t.Setenv("CHEAPSPACE_MAX_CPU_MILLIS", "8000")
	t.Setenv("CHEAPSPACE_MAX_MEMORY_MB", "16384")
	t.Setenv("CHEAPSPACE_MAX_TTL_MINUTES", "1440")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{
		"workspace", "new",
		"--name", "cli-test",
		"--repo-url", "https://github.com/example/repo.git",
		"--repo-branch", "main",
		"--ssh-port", "2304",
		"--cpu-cores", "2",
		"--memory-mb", "1024",
		"--ttl-minutes", "15",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run(workspace new) error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ssh -p 2304 codespace@localhost") {
		t.Fatalf("expected SSH command in output, got %q", stdout.String())
	}

	matches := regexp.MustCompile(`Workspace (ws-[^\s]+) is`).FindStringSubmatch(stdout.String())
	if len(matches) != 2 {
		t.Fatalf("expected workspace ID in output, got %q", stdout.String())
	}
	workspaceID := matches[1]

	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"workspace", "list"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(workspace list) error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cli-test") || !strings.Contains(stdout.String(), "main") {
		t.Fatalf("expected created workspace in list output, got %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"workspace", "view", workspaceID}, &stdout, &stderr); err != nil {
		t.Fatalf("run(workspace view) error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Branch: main") || !strings.Contains(stdout.String(), "CPU: 2 cores") {
		t.Fatalf("expected branch and CPU in view output, got %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"workspace", "delete", workspaceID}, &stdout, &stderr); err != nil {
		t.Fatalf("run(workspace delete) error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "deleted") {
		t.Fatalf("expected delete confirmation, got %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"workspace", "list"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(workspace list after delete) error = %v, stderr = %s", err, stderr.String())
	}
	if strings.Contains(stdout.String(), workspaceID) {
		t.Fatalf("expected deleted workspace to be hidden, got %q", stdout.String())
	}
}
