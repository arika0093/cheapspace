package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type MockRuntime struct{}

func NewMock() Runtime {
	return &MockRuntime{}
}

func (m *MockRuntime) Kind() string {
	return "mock"
}

func (m *MockRuntime) Build(ctx context.Context, req BuildRequest, control Control, logger Logger) (BuildResult, error) {
	steps := []string{
		"Inspecting requested environment source",
		"Preparing mock build context",
		"Generating resolved image reference",
	}
	for _, step := range steps {
		if err := waitStep(ctx, control, logger, step); err != nil {
			return BuildResult{}, err
		}
	}

	result := BuildResult{}
	switch req.SourceType {
	case "builtin_image":
		result.ResolvedImageRef = req.DefaultImage
	case "image_ref":
		result.ResolvedImageRef = req.SourceRef
	case "dockerfile":
		result.ResolvedImageRef = "cheapspace/mock-dockerfile:" + strings.ToLower(req.WorkspaceID)
	case "nixpacks":
		result.ResolvedImageRef = "cheapspace/mock-nixpacks:" + strings.ToLower(req.WorkspaceID)
		result.NixpacksPlanJSON = `{"provider":"mock","plan":["install","build","start"]}`
	default:
		return BuildResult{}, fmt.Errorf("unsupported mock source type %q", req.SourceType)
	}

	logger.Log("stdout", fmt.Sprintf("Resolved image %s", result.ResolvedImageRef))
	return result, nil
}

func (m *MockRuntime) Provision(ctx context.Context, req ProvisionRequest, control Control, logger Logger) (ProvisionResult, error) {
	steps := []string{
		"Creating workspace volume",
		"Starting workspace container",
		"Publishing SSH endpoint",
		"Workspace marked ready",
	}
	for _, step := range steps {
		if err := waitStep(ctx, control, logger, step); err != nil {
			return ProvisionResult{}, err
		}
	}

	logger.Log("stdout", fmt.Sprintf("Workspace %s ready on %s:%d", req.Name, req.PublicHost, req.SSHPort))
	return ProvisionResult{
		ContainerID:    "mock-" + req.WorkspaceID,
		ContainerName:  "cheapspace-" + req.WorkspaceID,
		VolumeName:     "cheapspace-ws-" + req.WorkspaceID,
		NetworkName:    req.WorkspaceNetwork,
		SSHHost:        req.PublicHost,
		SSHPort:        req.SSHPort,
		PublicHostname: req.PublicHostname,
	}, nil
}

func (m *MockRuntime) Delete(ctx context.Context, req DeleteRequest, control Control, logger Logger) error {
	steps := []string{
		"Stopping workspace container",
		"Removing workspace volume",
		"Releasing runtime resources",
	}
	for _, step := range steps {
		if err := waitStep(ctx, control, logger, step); err != nil {
			return err
		}
	}
	logger.Log("stdout", fmt.Sprintf("Workspace %s removed", req.WorkspaceID))
	return nil
}

func waitStep(ctx context.Context, control Control, logger Logger, step string) error {
	if control != nil && control.Cancelled(ctx) {
		logger.Log("system", "Cancellation requested")
		return ErrCancelled
	}
	logger.Log("system", step)
	timer := time.NewTimer(180 * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
