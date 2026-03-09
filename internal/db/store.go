package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.Exec(`
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite pragmas: %w", err)
	}

	return db, nil
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

type CreateWorkspaceParams struct {
	Workspace Workspace
	SSHKeys   []WorkspaceSSHKey
	Secret    *EphemeralSecret
	Job       Job
	Event     WorkspaceEvent
}

func (s *Store) CreateWorkspace(ctx context.Context, params CreateWorkspaceParams) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if err = insertWorkspace(ctx, tx, params.Workspace); err != nil {
		return err
	}
	for _, key := range params.SSHKeys {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO workspace_ssh_keys(id, workspace_id, key_type, public_key, comment, created_at)
VALUES(?, ?, ?, ?, ?, ?)
`, key.ID, key.WorkspaceID, key.KeyType, key.PublicKey, key.Comment, timestamp(key.CreatedAt)); err != nil {
			return fmt.Errorf("insert ssh key: %w", err)
		}
	}
	if params.Secret != nil {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO ephemeral_secrets(id, workspace_id, secret_kind, ciphertext, expires_at, consumed_at, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
`, params.Secret.ID, params.Secret.WorkspaceID, params.Secret.SecretKind, params.Secret.Ciphertext, timestamp(params.Secret.ExpiresAt), nullableTimestamp(params.Secret.ConsumedAt), timestamp(params.Secret.CreatedAt)); err != nil {
			return fmt.Errorf("insert secret: %w", err)
		}
	}
	if err = insertJob(ctx, tx, params.Job); err != nil {
		return err
	}
	if err = insertWorkspaceEvent(ctx, tx, params.Event); err != nil {
		return err
	}

	err = tx.Commit()
	return err
}

func (s *Store) ListWorkspaces(ctx context.Context, includeDeleted bool) ([]Workspace, error) {
	query := `
SELECT id, name, state, repo_url, dotfiles_url, source_type, source_ref, resolved_image_ref, nixpacks_plan_json,
       repo_branch, http_proxy, https_proxy, no_proxy, proxy_pac_url, cpu_millis, memory_mb, ttl_minutes, runtime_kind, ssh_endpoint_mode,
       ssh_host, ssh_port, public_hostname, password_auth_enabled, password_hash, traefik_enabled, traefik_base_domain,
       container_id, container_name, volume_name, network_name, last_error, created_at, started_at, expires_at, deleted_at
FROM workspaces
`
	if !includeDeleted {
		query += "WHERE deleted_at IS NULL\n"
	}
	query += "ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, rows.Err()
}

func (s *Store) GetWorkspace(ctx context.Context, id string) (Workspace, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, state, repo_url, dotfiles_url, source_type, source_ref, resolved_image_ref, nixpacks_plan_json,
       repo_branch, http_proxy, https_proxy, no_proxy, proxy_pac_url, cpu_millis, memory_mb, ttl_minutes, runtime_kind, ssh_endpoint_mode,
       ssh_host, ssh_port, public_hostname, password_auth_enabled, password_hash, traefik_enabled, traefik_base_domain,
       container_id, container_name, volume_name, network_name, last_error, created_at, started_at, expires_at, deleted_at
FROM workspaces
WHERE id = ?
`, id)
	return scanWorkspace(row)
}

func (s *Store) ListWorkspaceSSHKeys(ctx context.Context, workspaceID string) ([]WorkspaceSSHKey, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, key_type, public_key, comment, created_at
FROM workspace_ssh_keys
WHERE workspace_id = ?
ORDER BY created_at ASC
`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []WorkspaceSSHKey
	for rows.Next() {
		var key WorkspaceSSHKey
		var createdAt string
		if err := rows.Scan(&key.ID, &key.WorkspaceID, &key.KeyType, &key.PublicKey, &key.Comment, &createdAt); err != nil {
			return nil, err
		}
		key.CreatedAt = mustTime(createdAt)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) ListWorkspaceEvents(ctx context.Context, workspaceID string, limit int) ([]WorkspaceEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, event_type, message, detail_json, created_at
FROM workspace_events
WHERE workspace_id = ?
ORDER BY created_at DESC
LIMIT ?
`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []WorkspaceEvent
	for rows.Next() {
		var item WorkspaceEvent
		var createdAt string
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.EventType, &item.Message, &item.DetailJSON, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = mustTime(createdAt)
		events = append(events, item)
	}
	return events, rows.Err()
}

func (s *Store) AddWorkspaceEvent(ctx context.Context, event WorkspaceEvent) error {
	return insertWorkspaceEvent(ctx, s.db, event)
}

func (s *Store) UpdateWorkspaceState(ctx context.Context, workspaceID, state, lastError string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE workspaces
SET state = ?, last_error = ?
WHERE id = ?
`, state, lastError, workspaceID)
	return err
}

func (s *Store) UpdateWorkspaceResolvedImage(ctx context.Context, workspaceID, resolvedImage, nixpacksPlan string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE workspaces
SET resolved_image_ref = ?, nixpacks_plan_json = ?, last_error = ''
WHERE id = ?
`, resolvedImage, nixpacksPlan, workspaceID)
	return err
}

func (s *Store) UpdateWorkspaceProvisioned(ctx context.Context, workspaceID, state, host string, port int, publicHostname, containerID, containerName, volumeName, networkName string, startedAt, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE workspaces
SET state = ?, ssh_host = ?, ssh_port = ?, public_hostname = ?, container_id = ?, container_name = ?, volume_name = ?, network_name = ?, started_at = ?, expires_at = ?, last_error = ''
WHERE id = ?
`, state, host, port, publicHostname, containerID, containerName, volumeName, networkName, timestamp(startedAt), timestamp(expiresAt), workspaceID)
	return err
}

func (s *Store) MarkWorkspaceDeleted(ctx context.Context, workspaceID string, deletedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE workspaces
SET state = 'deleted', deleted_at = ?, last_error = ''
WHERE id = ?
`, timestamp(deletedAt), workspaceID)
	return err
}

func (s *Store) GetPasswordSecret(ctx context.Context, workspaceID string) (*EphemeralSecret, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, workspace_id, secret_kind, ciphertext, expires_at, consumed_at, created_at
FROM ephemeral_secrets
WHERE workspace_id = ? AND secret_kind = 'ssh_password'
ORDER BY created_at DESC
LIMIT 1
`, workspaceID)

	var secret EphemeralSecret
	var expiresAt, consumedAt sql.NullString
	var createdAt string
	err := row.Scan(&secret.ID, &secret.WorkspaceID, &secret.SecretKind, &secret.Ciphertext, &expiresAt, &consumedAt, &createdAt)
	if err != nil {
		return nil, err
	}
	secret.ExpiresAt = mustTime(expiresAt.String)
	secret.ConsumedAt = parseNullableTime(consumedAt)
	secret.CreatedAt = mustTime(createdAt)
	return &secret, nil
}

func (s *Store) MarkSecretConsumed(ctx context.Context, secretID string, consumedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE ephemeral_secrets
SET consumed_at = ?
WHERE id = ?
`, timestamp(consumedAt), secretID)
	return err
}

func (s *Store) CreateJob(ctx context.Context, job Job) error {
	return insertJob(ctx, s.db, job)
}

func (s *Store) ListJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, job_type, status, payload_json, attempt, latest_log, cancel_requested, last_error, created_at, updated_at, started_at, finished_at, run_after
FROM jobs
ORDER BY created_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) ListJobsForWorkspace(ctx context.Context, workspaceID string) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, workspace_id, job_type, status, payload_json, attempt, latest_log, cancel_requested, last_error, created_at, updated_at, started_at, finished_at, run_after
FROM jobs
WHERE workspace_id = ?
ORDER BY created_at DESC
`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) GetJob(ctx context.Context, jobID string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, workspace_id, job_type, status, payload_json, attempt, latest_log, cancel_requested, last_error, created_at, updated_at, started_at, finished_at, run_after
FROM jobs
WHERE id = ?
`, jobID)
	return scanJob(row)
}

func (s *Store) ListJobLogs(ctx context.Context, jobID string, afterSequence int) ([]JobLog, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, sequence_no, stream, message, created_at
FROM job_logs
WHERE job_id = ? AND sequence_no > ?
ORDER BY sequence_no ASC
`, jobID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []JobLog
	for rows.Next() {
		var item JobLog
		var createdAt string
		if err := rows.Scan(&item.ID, &item.JobID, &item.Sequence, &item.Stream, &item.Message, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = mustTime(createdAt)
		logs = append(logs, item)
	}
	return logs, rows.Err()
}

func (s *Store) AppendJobLog(ctx context.Context, jobID, stream, message string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var nextSequence int
	if scanErr := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence_no), 0) + 1 FROM job_logs WHERE job_id = ?`, jobID).Scan(&nextSequence); scanErr != nil {
		err = scanErr
		return err
	}

	if _, err = tx.ExecContext(ctx, `
INSERT INTO job_logs(id, job_id, sequence_no, stream, message, created_at)
VALUES(?, ?, ?, ?, ?, ?)
`, randomID("log"), jobID, nextSequence, stream, message, timestamp(time.Now())); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `
UPDATE jobs
SET latest_log = ?, updated_at = ?
WHERE id = ?
`, message, timestamp(time.Now()), jobID); err != nil {
		return err
	}

	err = tx.Commit()
	return err
}

func (s *Store) LeaseNextJob(ctx context.Context) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	row := tx.QueryRowContext(ctx, `
SELECT id, workspace_id, job_type, status, payload_json, attempt, latest_log, cancel_requested, last_error, created_at, updated_at, started_at, finished_at, run_after
FROM jobs
WHERE status = 'queued' AND run_after <= ?
ORDER BY run_after ASC, created_at ASC
LIMIT 1
`, timestamp(time.Now()))

	job, scanErr := scanJob(row)
	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			_ = tx.Rollback()
			return nil, nil
		}
		err = scanErr
		return nil, err
	}

	startedAt := time.Now()
	if _, err = tx.ExecContext(ctx, `
UPDATE jobs
SET status = 'running', updated_at = ?, started_at = ?
WHERE id = ?
`, timestamp(startedAt), timestamp(startedAt), job.ID); err != nil {
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}
	job.Status = "running"
	job.UpdatedAt = startedAt
	job.StartedAt = &startedAt
	return &job, nil
}

func (s *Store) SetJobStatus(ctx context.Context, jobID, status, lastError string) error {
	finishedAt := sql.NullString{}
	if status == "done" || status == "failed" || status == "cancelled" {
		finishedAt = sql.NullString{String: timestamp(time.Now()), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs
SET status = ?, last_error = ?, updated_at = ?, finished_at = CASE WHEN ? != '' THEN ? ELSE finished_at END
WHERE id = ?
`, status, lastError, timestamp(time.Now()), finishedAt.String, finishedAt.String, jobID)
	return err
}

func (s *Store) RequestJobCancel(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs
SET cancel_requested = 1, updated_at = ?
WHERE id = ?
`, timestamp(time.Now()), jobID)
	return err
}

func (s *Store) ClearJobCancel(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE jobs
SET cancel_requested = 0, updated_at = ?
WHERE id = ?
`, timestamp(time.Now()), jobID)
	return err
}

func (s *Store) DeleteJob(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = ? AND status != 'running'`, jobID)
	return err
}

func (s *Store) AllocatePort(ctx context.Context, workspaceID string, preferredPort, rangeStart, rangeEnd int) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var existingPort int
	existingErr := tx.QueryRowContext(ctx, `
SELECT port
FROM port_leases
WHERE workspace_id = ? AND released_at IS NULL
LIMIT 1
`, workspaceID).Scan(&existingPort)
	if existingErr == nil {
		if err = tx.Commit(); err != nil {
			return 0, err
		}
		return existingPort, nil
	}
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return 0, existingErr
	}

	now := timestamp(time.Now())
	for _, port := range candidatePorts(preferredPort, rangeStart, rangeEnd) {
		var currentWorkspaceID string
		var releasedAt sql.NullString
		scanErr := tx.QueryRowContext(ctx, `
SELECT workspace_id, released_at
FROM port_leases
WHERE port = ?
`, port).Scan(&currentWorkspaceID, &releasedAt)
		switch {
		case errors.Is(scanErr, sql.ErrNoRows):
			if _, err = tx.ExecContext(ctx, `
INSERT INTO port_leases(port, workspace_id, leased_at, released_at)
VALUES(?, ?, ?, NULL)
`, port, workspaceID, now); err != nil {
				return 0, err
			}
		case scanErr != nil:
			return 0, scanErr
		case !releasedAt.Valid && currentWorkspaceID != workspaceID:
			continue
		case !releasedAt.Valid && currentWorkspaceID == workspaceID:
		default:
			if _, err = tx.ExecContext(ctx, `
UPDATE port_leases
SET workspace_id = ?, leased_at = ?, released_at = NULL
WHERE port = ?
`, workspaceID, now, port); err != nil {
				return 0, err
			}
		}
		if err = tx.Commit(); err != nil {
			return 0, err
		}
		return port, nil
	}

	if preferredPort > 0 {
		return 0, fmt.Errorf("requested port %d is unavailable", preferredPort)
	}
	return 0, fmt.Errorf("no free ports available in %d-%d", rangeStart, rangeEnd)
}

func (s *Store) ReleasePort(ctx context.Context, workspaceID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE port_leases
SET released_at = ?
WHERE workspace_id = ? AND released_at IS NULL
`, timestamp(time.Now()), workspaceID)
	return err
}

func insertWorkspace(ctx context.Context, execer execer, workspace Workspace) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO workspaces(
    id, name, state, repo_url, dotfiles_url, source_type, source_ref, resolved_image_ref, nixpacks_plan_json,
    repo_branch, http_proxy, https_proxy, no_proxy, proxy_pac_url, cpu_millis, memory_mb, ttl_minutes, runtime_kind, ssh_endpoint_mode,
    ssh_host, ssh_port, public_hostname, password_auth_enabled, password_hash, traefik_enabled, traefik_base_domain,
    container_id, container_name, volume_name, network_name, last_error, created_at, started_at, expires_at, deleted_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, workspace.ID, workspace.Name, workspace.State, workspace.RepoURL, workspace.DotfilesURL, workspace.SourceType, workspace.SourceRef, workspace.ResolvedImageRef, workspace.NixpacksPlanJSON, workspace.RepoBranch, workspace.HTTPProxy, workspace.HTTPSProxy, workspace.NoProxy, workspace.ProxyPACURL, workspace.CPUMillis, workspace.MemoryMB, workspace.TTLMinutes, workspace.RuntimeKind, workspace.SSHEndpointMode, workspace.SSHHost, workspace.SSHPort, workspace.PublicHostname, boolToInt(workspace.PasswordAuthEnabled), workspace.PasswordHash, boolToInt(workspace.TraefikEnabled), workspace.TraefikBaseDomain, workspace.ContainerID, workspace.ContainerName, workspace.VolumeName, workspace.NetworkName, workspace.LastError, timestamp(workspace.CreatedAt), nullableTimestamp(workspace.StartedAt), nullableTimestamp(workspace.ExpiresAt), nullableTimestamp(workspace.DeletedAt))
	return err
}

func insertWorkspaceEvent(ctx context.Context, execer execer, event WorkspaceEvent) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO workspace_events(id, workspace_id, event_type, message, detail_json, created_at)
VALUES(?, ?, ?, ?, ?, ?)
`, event.ID, event.WorkspaceID, event.EventType, event.Message, event.DetailJSON, timestamp(event.CreatedAt))
	return err
}

func insertJob(ctx context.Context, execer execer, job Job) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO jobs(id, workspace_id, job_type, status, payload_json, attempt, latest_log, cancel_requested, last_error, created_at, updated_at, started_at, finished_at, run_after)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, job.ID, job.WorkspaceID, job.JobType, job.Status, job.PayloadJSON, job.Attempt, job.LatestLog, boolToInt(job.CancelRequested), job.LastError, timestamp(job.CreatedAt), timestamp(job.UpdatedAt), nullableTimestamp(job.StartedAt), nullableTimestamp(job.FinishedAt), timestamp(job.RunAfter))
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func scanWorkspace(sc scanner) (Workspace, error) {
	var item Workspace
	var createdAt string
	var startedAt, expiresAt, deletedAt sql.NullString
	var passwordAuthEnabled, traefikEnabled int
	err := sc.Scan(&item.ID, &item.Name, &item.State, &item.RepoURL, &item.DotfilesURL, &item.SourceType, &item.SourceRef, &item.ResolvedImageRef, &item.NixpacksPlanJSON, &item.RepoBranch, &item.HTTPProxy, &item.HTTPSProxy, &item.NoProxy, &item.ProxyPACURL, &item.CPUMillis, &item.MemoryMB, &item.TTLMinutes, &item.RuntimeKind, &item.SSHEndpointMode, &item.SSHHost, &item.SSHPort, &item.PublicHostname, &passwordAuthEnabled, &item.PasswordHash, &traefikEnabled, &item.TraefikBaseDomain, &item.ContainerID, &item.ContainerName, &item.VolumeName, &item.NetworkName, &item.LastError, &createdAt, &startedAt, &expiresAt, &deletedAt)
	if err != nil {
		return Workspace{}, err
	}
	item.PasswordAuthEnabled = passwordAuthEnabled == 1
	item.TraefikEnabled = traefikEnabled == 1
	item.CreatedAt = mustTime(createdAt)
	item.StartedAt = parseNullableTime(startedAt)
	item.ExpiresAt = parseNullableTime(expiresAt)
	item.DeletedAt = parseNullableTime(deletedAt)
	return item, nil
}

func candidatePorts(preferredPort, rangeStart, rangeEnd int) []int {
	if preferredPort > 0 {
		return []int{preferredPort}
	}
	candidates := make([]int, 0, rangeEnd-rangeStart+1)
	for port := rangeStart; port <= rangeEnd; port++ {
		candidates = append(candidates, port)
	}
	return candidates
}

func scanJob(sc scanner) (Job, error) {
	var item Job
	var createdAt, updatedAt, runAfter string
	var startedAt, finishedAt sql.NullString
	var cancelRequested int
	err := sc.Scan(&item.ID, &item.WorkspaceID, &item.JobType, &item.Status, &item.PayloadJSON, &item.Attempt, &item.LatestLog, &cancelRequested, &item.LastError, &createdAt, &updatedAt, &startedAt, &finishedAt, &runAfter)
	if err != nil {
		return Job{}, err
	}
	item.CancelRequested = cancelRequested == 1
	item.CreatedAt = mustTime(createdAt)
	item.UpdatedAt = mustTime(updatedAt)
	item.StartedAt = parseNullableTime(startedAt)
	item.FinishedAt = parseNullableTime(finishedAt)
	item.RunAfter = mustTime(runAfter)
	return item, nil
}

func timestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func nullableTimestamp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return timestamp(*t)
}

func parseNullableTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed := mustTime(value.String)
	return &parsed
}

func mustTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func randomID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "-" + timestamp(time.Now())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}
