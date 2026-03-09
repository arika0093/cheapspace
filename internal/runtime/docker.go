package runtime

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type DockerRuntime struct {
	dockerBinary     string
	gitBinary        string
	nixpacksBinary   string
	workspaceNetwork string
}

func NewDocker(dockerBinary, gitBinary, nixpacksBinary, workspaceNetwork string) Runtime {
	return &DockerRuntime{
		dockerBinary:     dockerBinary,
		gitBinary:        gitBinary,
		nixpacksBinary:   nixpacksBinary,
		workspaceNetwork: workspaceNetwork,
	}
}

func (d *DockerRuntime) Kind() string {
	return "docker"
}

func (d *DockerRuntime) Build(ctx context.Context, req BuildRequest, control Control, logger Logger) (BuildResult, error) {
	if control != nil && control.Cancelled(ctx) {
		return BuildResult{}, ErrCancelled
	}

	switch req.SourceType {
	case "builtin_image":
		logger.Log("system", "Using configured built-in workspace image")
		return BuildResult{ResolvedImageRef: req.DefaultImage}, nil
	case "image_ref":
		logger.Log("system", "Using requested image reference")
		return BuildResult{ResolvedImageRef: req.SourceRef}, nil
	case "dockerfile":
		return d.buildFromDockerfile(ctx, req, logger)
	case "nixpacks":
		return d.buildWithNixpacks(ctx, req, logger)
	default:
		return BuildResult{}, fmt.Errorf("unsupported source type %q", req.SourceType)
	}
}

func (d *DockerRuntime) buildFromDockerfile(ctx context.Context, req BuildRequest, logger Logger) (BuildResult, error) {
	tmpDir, err := os.MkdirTemp("", "cheapspace-build-*")
	if err != nil {
		return BuildResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(req.SourceRef), 0o644); err != nil {
		return BuildResult{}, err
	}

	tag := dockerSafeName("cheapspace-" + req.WorkspaceID + "-dockerfile")
	if err := d.runCommand(ctx, logger, tmpDir, d.dockerBinary, "build", "-t", tag, "."); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{ResolvedImageRef: tag}, nil
}

func (d *DockerRuntime) buildWithNixpacks(ctx context.Context, req BuildRequest, logger Logger) (BuildResult, error) {
	if req.RepoURL == "" {
		return BuildResult{}, errors.New("nixpacks build requires a repository URL")
	}

	tmpDir, err := os.MkdirTemp("", "cheapspace-nixpacks-*")
	if err != nil {
		return BuildResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	appDir := filepath.Join(tmpDir, "repo")
	if err := d.runCommand(ctx, logger, tmpDir, d.gitBinary, "clone", "--depth", "1", req.RepoURL, appDir); err != nil {
		return BuildResult{}, err
	}

	tag := dockerSafeName("cheapspace-" + req.WorkspaceID + "-nixpacks")
	if err := d.runCommand(ctx, logger, appDir, d.nixpacksBinary, "build", ".", "--name", tag); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{
		ResolvedImageRef: tag,
		NixpacksPlanJSON: `{"provider":"nixpacks","status":"built"}`,
	}, nil
}

func (d *DockerRuntime) Provision(ctx context.Context, req ProvisionRequest, control Control, logger Logger) (ProvisionResult, error) {
	if control != nil && control.Cancelled(ctx) {
		return ProvisionResult{}, ErrCancelled
	}

	if req.ResolvedImageRef == "" {
		return ProvisionResult{}, errors.New("resolved image reference is required for provisioning")
	}

	containerName := dockerSafeName("cheapspace-" + req.WorkspaceID)
	volumeName := dockerSafeName("cheapspace-ws-" + req.WorkspaceID)
	networkName := d.workspaceNetwork
	if req.WorkspaceNetwork != "" {
		networkName = req.WorkspaceNetwork
	}

	if networkName != "" && networkName != "bridge" {
		_ = d.runCommand(ctx, logger, "", d.dockerBinary, "network", "inspect", networkName)
	}

	if err := d.runCommand(ctx, logger, "", d.dockerBinary, "volume", "create", volumeName); err != nil {
		return ProvisionResult{}, err
	}
	logger.Log("system", fmt.Sprintf("Using workspace image %s", req.ResolvedImageRef))

	args := []string{
		"create",
		"--name", containerName,
		"-p", fmt.Sprintf("%d:22", req.SSHPort),
		"-v", fmt.Sprintf("%s:/workspaces", volumeName),
	}
	if req.CPUMillis > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(float64(req.CPUMillis)/1000.0, 'f', 3, 64))
	}
	if req.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", req.MemoryMB))
	}
	if networkName != "" {
		args = append(args, "--network", networkName)
	}
	for _, env := range provisionEnv(req) {
		args = append(args, "-e", env)
	}
	for key, value := range provisionLabels(req) {
		args = append(args, "-l", fmt.Sprintf("%s=%s", key, value))
	}
	args = append(args, req.ResolvedImageRef)
	logger.Log("system", fmt.Sprintf("Creating container %s", containerName))

	var output bytes.Buffer
	if err := d.runCommandToBuffer(ctx, logger, &output, "", d.dockerBinary, args...); err != nil {
		return ProvisionResult{}, err
	}
	containerID := strings.TrimSpace(output.String())
	if containerID == "" {
		containerID = containerName
	}

	if control != nil && control.Cancelled(ctx) {
		return ProvisionResult{}, ErrCancelled
	}

	logger.Log("system", fmt.Sprintf("Starting container %s", containerName))
	if err := d.runCommand(ctx, logger, "", d.dockerBinary, "start", containerName); err != nil {
		return ProvisionResult{}, err
	}

	return ProvisionResult{
		ContainerID:    containerID,
		ContainerName:  containerName,
		VolumeName:     volumeName,
		NetworkName:    networkName,
		SSHHost:        req.PublicHost,
		SSHPort:        req.SSHPort,
		PublicHostname: req.PublicHostname,
	}, nil
}

func (d *DockerRuntime) Delete(ctx context.Context, req DeleteRequest, control Control, logger Logger) error {
	if control != nil && control.Cancelled(ctx) {
		return ErrCancelled
	}
	target := req.ContainerName
	if target == "" {
		target = req.ContainerID
	}
	if target != "" {
		_ = d.runCommand(ctx, logger, "", d.dockerBinary, "rm", "-f", target)
	}
	if req.VolumeName != "" {
		_ = d.runCommand(ctx, logger, "", d.dockerBinary, "volume", "rm", "-f", req.VolumeName)
	}
	return nil
}

func (d *DockerRuntime) runCommand(ctx context.Context, logger Logger, dir string, binary string, args ...string) error {
	var sink bytes.Buffer
	return d.runCommandToBuffer(ctx, logger, &sink, dir, binary, args...)
}

func (d *DockerRuntime) runCommandToBuffer(ctx context.Context, logger Logger, output *bytes.Buffer, dir string, binary string, args ...string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	if err := cmd.Run(); err != nil {
		scanLogs(logger, combined.Bytes())
		message := strings.TrimSpace(combined.String())
		if message != "" {
			return fmt.Errorf("%s %s: %s: %w", binary, strings.Join(args, " "), message, err)
		}
		return fmt.Errorf("%s %s: %w", binary, strings.Join(args, " "), err)
	}

	scanLogs(logger, combined.Bytes())
	if output != nil {
		output.Write(combined.Bytes())
	}
	return nil
}

func provisionEnv(req ProvisionRequest) []string {
	values := []string{
		"CHEAPSPACE_REPO_URL=" + req.RepoURL,
		"CHEAPSPACE_DOTFILES_URL=" + req.DotfilesURL,
		"CHEAPSPACE_HTTP_PROXY=" + req.HTTPProxy,
		"CHEAPSPACE_HTTPS_PROXY=" + req.HTTPSProxy,
		"CHEAPSPACE_NO_PROXY=" + req.NoProxy,
		"CHEAPSPACE_PROXY_PAC_URL=" + req.ProxyPACURL,
		"CHEAPSPACE_AUTHORIZED_KEYS=" + strings.Join(req.AuthorizedKeys, "\n"),
		"CHEAPSPACE_PASSWORD_AUTH_ENABLED=" + boolString(req.PasswordAuthEnabled),
		"CHEAPSPACE_PASSWORD=" + req.Password,
	}
	return values
}

func provisionLabels(req ProvisionRequest) map[string]string {
	labels := map[string]string{
		"cheapspace.workspace-id": req.WorkspaceID,
	}
	if req.PublicHostname != "" {
		labels["cheapspace.public-hostname"] = req.PublicHostname
	}
	if req.TraefikEnabled && req.PublicHostname != "" {
		router := dockerSafeName("workspace-" + req.WorkspaceID)
		labels["traefik.enable"] = "true"
		labels["traefik.http.routers."+router+".rule"] = "Host(`" + req.PublicHostname + "`)"
		labels["traefik.http.routers."+router+".entrypoints"] = "websecure"
		labels["traefik.http.routers."+router+".tls"] = "true"
		labels["traefik.http.services."+router+".loadbalancer.server.port"] = "3000"
	}
	return labels
}

func scanLogs(logger Logger, payload []byte) {
	if logger == nil || len(payload) == 0 {
		return
	}
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			logger.Log("stdout", line)
		}
	}
}

func dockerSafeName(input string) string {
	input = strings.ToLower(input)
	var b strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
