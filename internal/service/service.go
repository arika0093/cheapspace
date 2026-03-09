package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cheapspace/internal/config"
	"cheapspace/internal/db"
	"cheapspace/internal/runtime"
)

const (
	JobBuildImage         = "build_image"
	JobProvisionWorkspace = "provision_workspace"
	JobDeleteWorkspace    = "delete_workspace"
)

type Service struct {
	cfg     config.Config
	store   *db.Store
	runtime runtime.Runtime
	logger  *slog.Logger
}

type CreateWorkspaceInput struct {
	Name                string
	RepoURL             string
	DotfilesURL         string
	SourceType          string
	SourceRef           string
	SSHKeys             []string
	SSHPort             int
	PasswordAuthEnabled bool
	HTTPProxy           string
	HTTPSProxy          string
	NoProxy             string
	ProxyPACURL         string
	CPUMillis           int
	MemoryMB            int
	TTLMinutes          int
	TraefikEnabled      bool
	TraefikBaseDomain   string
}

type WorkspaceDetails struct {
	Workspace db.Workspace
	Keys      []db.WorkspaceSSHKey
	Events    []db.WorkspaceEvent
	Jobs      []db.Job
}

type JobDetails struct {
	Job       db.Job
	Workspace db.Workspace
	Logs      []db.JobLog
}

type jobPayload struct {
	WorkspaceID string `json:"workspace_id"`
}

func New(cfg config.Config, store *db.Store, rt runtime.Runtime) *Service {
	return &Service{
		cfg:     cfg,
		store:   store,
		runtime: rt,
		logger:  slog.Default(),
	}
}

func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			processed, err := s.RunOneJob(ctx)
			if err != nil {
				s.logger.Error("job execution failed", "err", err)
				time.Sleep(750 * time.Millisecond)
				continue
			}
			if !processed {
				time.Sleep(350 * time.Millisecond)
			}
		}
	}()
}

func (s *Service) RunOneJob(ctx context.Context) (bool, error) {
	job, err := s.store.LeaseNextJob(ctx)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}
	if err := s.executeJob(ctx, *job); err != nil {
		return true, err
	}
	return true, nil
}

func (s *Service) RunUntilIdle(ctx context.Context, maxIterations int) error {
	for i := 0; i < maxIterations; i++ {
		processed, err := s.RunOneJob(ctx)
		if err != nil {
			return err
		}
		if !processed {
			return nil
		}
	}
	return fmt.Errorf("jobs did not become idle after %d iterations", maxIterations)
}

func (s *Service) ListWorkspaces(ctx context.Context, includeDeleted bool) ([]db.Workspace, error) {
	return s.store.ListWorkspaces(ctx, includeDeleted)
}

func (s *Service) GetWorkspaceDetails(ctx context.Context, workspaceID string) (WorkspaceDetails, error) {
	workspace, err := s.store.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return WorkspaceDetails{}, err
	}
	keys, err := s.store.ListWorkspaceSSHKeys(ctx, workspaceID)
	if err != nil {
		return WorkspaceDetails{}, err
	}
	events, err := s.store.ListWorkspaceEvents(ctx, workspaceID, 50)
	if err != nil {
		return WorkspaceDetails{}, err
	}
	jobs, err := s.store.ListJobsForWorkspace(ctx, workspaceID)
	if err != nil {
		return WorkspaceDetails{}, err
	}
	return WorkspaceDetails{Workspace: workspace, Keys: keys, Events: events, Jobs: jobs}, nil
}

func (s *Service) ListJobs(ctx context.Context) ([]db.Job, error) {
	return s.store.ListJobs(ctx)
}

func (s *Service) GetJobDetails(ctx context.Context, jobID string, afterSequence int) (JobDetails, error) {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return JobDetails{}, err
	}
	workspace, err := s.store.GetWorkspace(ctx, job.WorkspaceID)
	if err != nil {
		return JobDetails{}, err
	}
	logs, err := s.store.ListJobLogs(ctx, jobID, afterSequence)
	if err != nil {
		return JobDetails{}, err
	}
	return JobDetails{Job: job, Workspace: workspace, Logs: logs}, nil
}

func (s *Service) CreateWorkspace(ctx context.Context, input CreateWorkspaceInput) (db.Workspace, error) {
	if input.SourceType == "" {
		input.SourceType = "builtin_image"
	}
	if input.SourceType == "builtin_image" {
		input.SourceRef = ""
	}
	input.HTTPProxy = strings.TrimSpace(input.HTTPProxy)
	input.HTTPSProxy = strings.TrimSpace(input.HTTPSProxy)
	if input.HTTPProxy != "" && input.HTTPSProxy == "" {
		input.HTTPSProxy = input.HTTPProxy
	}
	input.NoProxy = strings.TrimSpace(input.NoProxy)
	input.ProxyPACURL = strings.TrimSpace(input.ProxyPACURL)
	if input.CPUMillis <= 0 {
		input.CPUMillis = minInt(2000, s.cfg.MaxCPUMillis)
	}
	if input.MemoryMB <= 0 {
		input.MemoryMB = minInt(4096, s.cfg.MaxMemoryMB)
	}
	if input.TTLMinutes <= 0 {
		input.TTLMinutes = minInt(480, s.cfg.MaxTTLMinutes)
	}

	keys, err := normalizeSSHKeys(input.SSHKeys)
	if err != nil {
		return db.Workspace{}, err
	}
	input.PasswordAuthEnabled = len(keys) == 0

	if err := s.validateInput(input); err != nil {
		return db.Workspace{}, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(input.TTLMinutes) * time.Minute)
	workspaceID := randomID("ws")
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = friendlyName()
	}

	password := ""
	passwordHash := ""
	var secret *db.EphemeralSecret
	if input.PasswordAuthEnabled {
		password, err = generatePassword()
		if err != nil {
			return db.Workspace{}, err
		}
		passwordHash = hashPassword(password)
		ciphertext, err := encrypt(s.cfg.EncryptionKey(), password)
		if err != nil {
			return db.Workspace{}, err
		}
		secret = &db.EphemeralSecret{
			ID:          randomID("secret"),
			WorkspaceID: workspaceID,
			SecretKind:  "ssh_password",
			Ciphertext:  ciphertext,
			ExpiresAt:   now.Add(24 * time.Hour),
			CreatedAt:   now,
		}
	}

	traefikDomain := strings.TrimSpace(input.TraefikBaseDomain)
	if traefikDomain == "" {
		traefikDomain = s.cfg.TraefikBaseDomain
	}
	publicHostname := ""
	if input.TraefikEnabled && traefikDomain != "" {
		publicHostname = fmt.Sprintf("%s.%s", name, traefikDomain)
	}

	workspace := db.Workspace{
		ID:                  workspaceID,
		Name:                name,
		State:               "pending",
		RepoURL:             strings.TrimSpace(input.RepoURL),
		DotfilesURL:         strings.TrimSpace(input.DotfilesURL),
		SourceType:          input.SourceType,
		SourceRef:           input.SourceRef,
		ResolvedImageRef:    "",
		NixpacksPlanJSON:    "",
		HTTPProxy:           strings.TrimSpace(input.HTTPProxy),
		HTTPSProxy:          strings.TrimSpace(input.HTTPSProxy),
		NoProxy:             strings.TrimSpace(input.NoProxy),
		ProxyPACURL:         input.ProxyPACURL,
		CPUMillis:           input.CPUMillis,
		MemoryMB:            input.MemoryMB,
		TTLMinutes:          input.TTLMinutes,
		RuntimeKind:         s.runtime.Kind(),
		SSHEndpointMode:     "host_port",
		SSHPort:             input.SSHPort,
		PasswordAuthEnabled: input.PasswordAuthEnabled,
		PasswordHash:        passwordHash,
		TraefikEnabled:      input.TraefikEnabled,
		TraefikBaseDomain:   traefikDomain,
		PublicHostname:      publicHostname,
		CreatedAt:           now,
		ExpiresAt:           &expiresAt,
	}

	dbKeys := make([]db.WorkspaceSSHKey, 0, len(keys))
	for _, key := range keys {
		dbKeys = append(dbKeys, db.WorkspaceSSHKey{
			ID:          randomID("key"),
			WorkspaceID: workspaceID,
			KeyType:     sshKeyType(key),
			PublicKey:   key,
			Comment:     sshKeyComment(key),
			CreatedAt:   now,
		})
	}

	jobType := JobProvisionWorkspace
	if input.SourceType == "dockerfile" || input.SourceType == "nixpacks" {
		jobType = JobBuildImage
	}
	payloadJSON, err := json.Marshal(jobPayload{WorkspaceID: workspaceID})
	if err != nil {
		return db.Workspace{}, err
	}
	initialJob := db.Job{
		ID:          randomID("job"),
		WorkspaceID: workspaceID,
		JobType:     jobType,
		Status:      "queued",
		PayloadJSON: string(payloadJSON),
		Attempt:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
		RunAfter:    now,
	}

	event := db.WorkspaceEvent{
		ID:          randomID("evt"),
		WorkspaceID: workspaceID,
		EventType:   "workspace.created",
		Message:     "Workspace request accepted and queued",
		DetailJSON:  string(payloadJSON),
		CreatedAt:   now,
	}

	err = s.store.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Workspace: workspace,
		SSHKeys:   dbKeys,
		Secret:    secret,
		Job:       initialJob,
		Event:     event,
	})
	if err != nil {
		return db.Workspace{}, err
	}
	return workspace, nil
}

func (s *Service) QueueWorkspaceDelete(ctx context.Context, workspaceID string) error {
	workspace, err := s.store.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	payloadJSON, err := json.Marshal(jobPayload{WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	job := db.Job{
		ID:          randomID("job"),
		WorkspaceID: workspaceID,
		JobType:     JobDeleteWorkspace,
		Status:      "queued",
		PayloadJSON: string(payloadJSON),
		Attempt:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
		RunAfter:    now,
	}
	if err := s.store.CreateJob(ctx, job); err != nil {
		return err
	}
	if err := s.store.UpdateWorkspaceState(ctx, workspaceID, "deleting", workspace.LastError); err != nil {
		return err
	}
	return s.store.AddWorkspaceEvent(ctx, db.WorkspaceEvent{
		ID:          randomID("evt"),
		WorkspaceID: workspaceID,
		EventType:   "workspace.delete_requested",
		Message:     "Workspace delete job queued",
		CreatedAt:   now,
	})
}

func (s *Service) RequestJobCancel(ctx context.Context, jobID string) error {
	return s.store.RequestJobCancel(ctx, jobID)
}

func (s *Service) ResumeJob(ctx context.Context, jobID string) error {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status == "running" || job.Status == "queued" {
		return fmt.Errorf("job %s is already active", jobID)
	}
	now := time.Now().UTC()
	resumed := db.Job{
		ID:          randomID("job"),
		WorkspaceID: job.WorkspaceID,
		JobType:     job.JobType,
		Status:      "queued",
		PayloadJSON: job.PayloadJSON,
		Attempt:     job.Attempt + 1,
		CreatedAt:   now,
		UpdatedAt:   now,
		RunAfter:    now,
	}
	return s.store.CreateJob(ctx, resumed)
}

func (s *Service) DeleteJob(ctx context.Context, jobID string) error {
	return s.store.DeleteJob(ctx, jobID)
}

func (s *Service) RevealPassword(ctx context.Context, workspaceID string) (string, error) {
	secret, err := s.store.GetPasswordSecret(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if secret.ConsumedAt != nil {
		return "", fmt.Errorf("initial password already revealed")
	}
	password, err := decrypt(s.cfg.EncryptionKey(), secret.Ciphertext)
	if err != nil {
		return "", err
	}
	if err := s.store.MarkSecretConsumed(ctx, secret.ID, time.Now().UTC()); err != nil {
		return "", err
	}
	return password, nil
}

func (s *Service) executeJob(ctx context.Context, job db.Job) error {
	logger := jobLogger{ctx: ctx, store: s.store, jobID: job.ID}
	control := jobControl{ctx: ctx, store: s.store, jobID: job.ID}
	logger.Log("system", "Job started")

	switch job.JobType {
	case JobBuildImage:
		return s.executeBuildJob(ctx, job, control, logger)
	case JobProvisionWorkspace:
		return s.executeProvisionJob(ctx, job, control, logger)
	case JobDeleteWorkspace:
		return s.executeDeleteJob(ctx, job, control, logger)
	default:
		_ = s.store.SetJobStatus(ctx, job.ID, "failed", "unsupported job type")
		return fmt.Errorf("unsupported job type %q", job.JobType)
	}
}

func (s *Service) executeBuildJob(ctx context.Context, job db.Job, control jobControl, logger jobLogger) error {
	workspace, err := s.store.GetWorkspace(ctx, job.WorkspaceID)
	if err != nil {
		_ = s.store.SetJobStatus(ctx, job.ID, "failed", err.Error())
		return err
	}
	if err := s.store.UpdateWorkspaceState(ctx, workspace.ID, "building", ""); err != nil {
		return err
	}
	logger.Log("system", "Resolving workspace image")
	_ = s.store.AddWorkspaceEvent(ctx, db.WorkspaceEvent{
		ID:          randomID("evt"),
		WorkspaceID: workspace.ID,
		EventType:   "build.started",
		Message:     "Workspace image build started",
		CreatedAt:   time.Now().UTC(),
	})

	result, err := s.runtime.Build(ctx, runtime.BuildRequest{
		WorkspaceID:    workspace.ID,
		Name:           workspace.Name,
		SourceType:     workspace.SourceType,
		SourceRef:      workspace.SourceRef,
		RepoURL:        workspace.RepoURL,
		DotfilesURL:    workspace.DotfilesURL,
		DefaultImage:   s.cfg.DefaultWorkspaceImage,
		PublicHost:     s.cfg.PublicHost,
		PublicHostname: workspace.PublicHostname,
	}, control, logger)
	if err != nil {
		return s.failJob(ctx, job, workspace.ID, "build.failed", "Workspace image build failed", err)
	}

	if err := s.store.UpdateWorkspaceResolvedImage(ctx, workspace.ID, result.ResolvedImageRef, result.NixpacksPlanJSON); err != nil {
		return err
	}
	logger.Log("system", "Workspace image ready: "+result.ResolvedImageRef)
	if err := s.store.SetJobStatus(ctx, job.ID, "done", ""); err != nil {
		return err
	}
	_ = s.store.AddWorkspaceEvent(ctx, db.WorkspaceEvent{
		ID:          randomID("evt"),
		WorkspaceID: workspace.ID,
		EventType:   "build.completed",
		Message:     "Workspace image build completed",
		DetailJSON:  result.ResolvedImageRef,
		CreatedAt:   time.Now().UTC(),
	})

	return s.enqueueFollowUpProvision(ctx, workspace.ID)
}

func (s *Service) executeProvisionJob(ctx context.Context, job db.Job, control jobControl, logger jobLogger) error {
	workspace, err := s.store.GetWorkspace(ctx, job.WorkspaceID)
	if err != nil {
		_ = s.store.SetJobStatus(ctx, job.ID, "failed", err.Error())
		return err
	}
	if err := s.store.UpdateWorkspaceState(ctx, workspace.ID, "provisioning", ""); err != nil {
		return err
	}
	logger.Log("system", "Loading workspace access configuration")
	keys, err := s.store.ListWorkspaceSSHKeys(ctx, workspace.ID)
	if err != nil {
		return err
	}
	port, err := s.store.AllocatePort(ctx, workspace.ID, workspace.SSHPort, s.cfg.PortRangeStart, s.cfg.PortRangeEnd)
	if err != nil {
		return s.failJob(ctx, job, workspace.ID, "provision.failed", "Unable to allocate SSH port", err)
	}
	logger.Log("system", fmt.Sprintf("Reserved SSH host port %d", port))

	password := ""
	if workspace.PasswordAuthEnabled {
		secret, secretErr := s.store.GetPasswordSecret(ctx, workspace.ID)
		if secretErr != nil {
			return s.failJob(ctx, job, workspace.ID, "provision.failed", "Unable to load initial password", secretErr)
		}
		password, err = decrypt(s.cfg.EncryptionKey(), secret.Ciphertext)
		if err != nil {
			return s.failJob(ctx, job, workspace.ID, "provision.failed", "Unable to decrypt initial password", err)
		}
	}

	resolvedImage := workspace.ResolvedImageRef
	if resolvedImage == "" {
		switch workspace.SourceType {
		case "builtin_image":
			resolvedImage = s.cfg.DefaultWorkspaceImage
		case "image_ref":
			resolvedImage = workspace.SourceRef
		}
	}
	logger.Log("system", "Using workspace image "+resolvedImage)

	authKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		authKeys = append(authKeys, key.PublicKey)
	}

	expiresAt := time.Now().UTC().Add(time.Duration(workspace.TTLMinutes) * time.Minute)
	result, err := s.runtime.Provision(ctx, runtime.ProvisionRequest{
		WorkspaceID:         workspace.ID,
		Name:                workspace.Name,
		SourceType:          workspace.SourceType,
		SourceRef:           workspace.SourceRef,
		ResolvedImageRef:    resolvedImage,
		RepoURL:             workspace.RepoURL,
		DotfilesURL:         workspace.DotfilesURL,
		AuthorizedKeys:      authKeys,
		Password:            password,
		PasswordAuthEnabled: workspace.PasswordAuthEnabled,
		HTTPProxy:           workspace.HTTPProxy,
		HTTPSProxy:          workspace.HTTPSProxy,
		NoProxy:             workspace.NoProxy,
		ProxyPACURL:         workspace.ProxyPACURL,
		CPUMillis:           workspace.CPUMillis,
		MemoryMB:            workspace.MemoryMB,
		PublicHost:          s.cfg.PublicHost,
		PublicHostname:      workspace.PublicHostname,
		SSHPort:             port,
		TraefikEnabled:      workspace.TraefikEnabled,
		TraefikBaseDomain:   workspace.TraefikBaseDomain,
		WorkspaceNetwork:    s.cfg.WorkspaceNetwork,
	}, control, logger)
	if err != nil {
		_ = s.store.ReleasePort(ctx, workspace.ID)
		return s.failJob(ctx, job, workspace.ID, "provision.failed", "Workspace provisioning failed", err)
	}
	logger.Log("system", fmt.Sprintf("Workspace container is running and reachable on %s:%d", result.SSHHost, result.SSHPort))

	if err := s.store.UpdateWorkspaceProvisioned(ctx, workspace.ID, "running", result.SSHHost, result.SSHPort, result.PublicHostname, result.ContainerID, result.ContainerName, result.VolumeName, result.NetworkName, time.Now().UTC(), expiresAt); err != nil {
		return err
	}
	if err := s.store.SetJobStatus(ctx, job.ID, "done", ""); err != nil {
		return err
	}
	_ = s.store.AddWorkspaceEvent(ctx, db.WorkspaceEvent{
		ID:          randomID("evt"),
		WorkspaceID: workspace.ID,
		EventType:   "workspace.ready",
		Message:     "Workspace is ready for access",
		DetailJSON:  result.PublicHostname,
		CreatedAt:   time.Now().UTC(),
	})
	return nil
}

func (s *Service) executeDeleteJob(ctx context.Context, job db.Job, control jobControl, logger jobLogger) error {
	workspace, err := s.store.GetWorkspace(ctx, job.WorkspaceID)
	if err != nil {
		_ = s.store.SetJobStatus(ctx, job.ID, "failed", err.Error())
		return err
	}
	logger.Log("system", "Removing workspace container resources")

	if err := s.runtime.Delete(ctx, runtime.DeleteRequest{
		WorkspaceID:   workspace.ID,
		ContainerID:   workspace.ContainerID,
		ContainerName: workspace.ContainerName,
		VolumeName:    workspace.VolumeName,
		NetworkName:   workspace.NetworkName,
	}, control, logger); err != nil {
		return s.failJob(ctx, job, workspace.ID, "delete.failed", "Workspace deletion failed", err)
	}

	if err := s.store.ReleasePort(ctx, workspace.ID); err != nil {
		return err
	}
	if err := s.store.MarkWorkspaceDeleted(ctx, workspace.ID, time.Now().UTC()); err != nil {
		return err
	}
	if err := s.store.SetJobStatus(ctx, job.ID, "done", ""); err != nil {
		return err
	}
	return s.store.AddWorkspaceEvent(ctx, db.WorkspaceEvent{
		ID:          randomID("evt"),
		WorkspaceID: workspace.ID,
		EventType:   "workspace.deleted",
		Message:     "Workspace deleted",
		CreatedAt:   time.Now().UTC(),
	})
}

func (s *Service) enqueueFollowUpProvision(ctx context.Context, workspaceID string) error {
	payloadJSON, err := json.Marshal(jobPayload{WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.store.CreateJob(ctx, db.Job{
		ID:          randomID("job"),
		WorkspaceID: workspaceID,
		JobType:     JobProvisionWorkspace,
		Status:      "queued",
		PayloadJSON: string(payloadJSON),
		Attempt:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
		RunAfter:    now,
	})
}

func (s *Service) failJob(ctx context.Context, job db.Job, workspaceID, eventType, message string, cause error) error {
	if errors.Is(cause, runtime.ErrCancelled) {
		_ = s.store.SetJobStatus(ctx, job.ID, "cancelled", "user requested stop")
		_ = s.store.UpdateWorkspaceState(ctx, workspaceID, "pending", "")
		_ = s.store.AddWorkspaceEvent(ctx, db.WorkspaceEvent{
			ID:          randomID("evt"),
			WorkspaceID: workspaceID,
			EventType:   "job.cancelled",
			Message:     "Workspace job was cancelled",
			CreatedAt:   time.Now().UTC(),
		})
		return nil
	}

	_ = s.store.SetJobStatus(ctx, job.ID, "failed", cause.Error())
	_ = s.store.UpdateWorkspaceState(ctx, workspaceID, "failed", cause.Error())
	_ = s.store.AddWorkspaceEvent(ctx, db.WorkspaceEvent{
		ID:          randomID("evt"),
		WorkspaceID: workspaceID,
		EventType:   eventType,
		Message:     message,
		DetailJSON:  cause.Error(),
		CreatedAt:   time.Now().UTC(),
	})
	return cause
}

func (s *Service) validateInput(input CreateWorkspaceInput) error {
	switch input.SourceType {
	case "builtin_image", "image_ref", "dockerfile", "nixpacks":
	default:
		return fmt.Errorf("unsupported source type %q", input.SourceType)
	}
	if input.SourceType == "image_ref" && strings.TrimSpace(input.SourceRef) == "" {
		return errors.New("image reference is required")
	}
	if input.SourceType == "dockerfile" && strings.TrimSpace(input.SourceRef) == "" {
		return errors.New("Dockerfile contents are required")
	}
	if input.SourceType == "nixpacks" && strings.TrimSpace(input.RepoURL) == "" {
		return errors.New("repository URL is required for Nixpacks")
	}
	if input.CPUMillis <= 0 || input.CPUMillis > s.cfg.MaxCPUMillis {
		return fmt.Errorf("cpu must be between 1 and %d millicores", s.cfg.MaxCPUMillis)
	}
	if input.MemoryMB <= 0 || input.MemoryMB > s.cfg.MaxMemoryMB {
		return fmt.Errorf("memory must be between 1 and %d MB", s.cfg.MaxMemoryMB)
	}
	if input.TTLMinutes <= 0 || input.TTLMinutes > s.cfg.MaxTTLMinutes {
		return fmt.Errorf("ttl must be between 1 and %d minutes", s.cfg.MaxTTLMinutes)
	}
	if input.SSHPort != 0 && (input.SSHPort < s.cfg.PortRangeStart || input.SSHPort > s.cfg.PortRangeEnd) {
		return fmt.Errorf("ssh port must be between %d and %d", s.cfg.PortRangeStart, s.cfg.PortRangeEnd)
	}
	return nil
}

type jobControl struct {
	ctx   context.Context
	store *db.Store
	jobID string
}

func (c jobControl) Cancelled(ctx context.Context) bool {
	job, err := c.store.GetJob(ctx, c.jobID)
	if err != nil {
		return false
	}
	return job.CancelRequested
}

type jobLogger struct {
	ctx   context.Context
	store *db.Store
	jobID string
}

func (l jobLogger) Log(stream, message string) {
	_ = l.store.AppendJobLog(l.ctx, l.jobID, stream, message)
}

func normalizeSSHKeys(keys []string) ([]string, error) {
	seen := map[string]struct{}{}
	var normalized []string
	for _, raw := range keys {
		for _, line := range strings.Split(raw, "\n") {
			value := strings.TrimSpace(line)
			if value == "" {
				continue
			}
			parts := strings.Fields(value)
			if len(parts) < 2 {
				return nil, fmt.Errorf("invalid ssh public key: %q", value)
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			normalized = append(normalized, value)
		}
	}
	return normalized, nil
}

func sshKeyType(key string) string {
	parts := strings.Fields(key)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func sshKeyComment(key string) string {
	parts := strings.Fields(key)
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[2:], " ")
}

func generatePassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@$%*"
	bytes := make([]byte, 18)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, value := range bytes {
		b.WriteByte(alphabet[int(value)%len(alphabet)])
	}
	return b.String(), nil
}

func hashPassword(password string) string {
	salt := randomID("salt")
	sum := sha256.Sum256([]byte(salt + password))
	return salt + ":" + hex.EncodeToString(sum[:])
}

func encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
	buf := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(buf), nil
}

func decrypt(key []byte, encoded string) (string, error) {
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < aead.NonceSize() {
		return "", errors.New("invalid encrypted payload")
	}
	nonce := payload[:aead.NonceSize()]
	ciphertext := payload[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func randomID(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

func friendlyName() string {
	adjectives := []string{"aurora", "brisk", "cobalt", "ember", "lunar", "misty", "nova", "sunny"}
	nouns := []string{"otter", "falcon", "harbor", "meadow", "orbit", "summit", "vector", "willow"}
	a := adjectives[randomIndex(len(adjectives))]
	b := nouns[randomIndex(len(nouns))]
	return fmt.Sprintf("%s-%s", a, b)
}

func randomIndex(max int) int {
	buf := make([]byte, 1)
	if _, err := rand.Read(buf); err != nil || max <= 0 {
		return 0
	}
	return int(buf[0]) % max
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
