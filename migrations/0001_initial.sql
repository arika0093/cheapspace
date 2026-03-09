CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    state TEXT NOT NULL,
    repo_url TEXT NOT NULL DEFAULT '',
    dotfiles_url TEXT NOT NULL DEFAULT '',
    source_type TEXT NOT NULL,
    source_ref TEXT NOT NULL DEFAULT '',
    resolved_image_ref TEXT NOT NULL DEFAULT '',
    nixpacks_plan_json TEXT NOT NULL DEFAULT '',
    http_proxy TEXT NOT NULL DEFAULT '',
    https_proxy TEXT NOT NULL DEFAULT '',
    no_proxy TEXT NOT NULL DEFAULT '',
    cpu_millis INTEGER NOT NULL DEFAULT 2000,
    memory_mb INTEGER NOT NULL DEFAULT 4096,
    ttl_minutes INTEGER NOT NULL DEFAULT 480,
    runtime_kind TEXT NOT NULL DEFAULT 'mock',
    ssh_endpoint_mode TEXT NOT NULL DEFAULT 'host_port',
    ssh_host TEXT NOT NULL DEFAULT '',
    ssh_port INTEGER NOT NULL DEFAULT 0,
    public_hostname TEXT NOT NULL DEFAULT '',
    password_auth_enabled INTEGER NOT NULL DEFAULT 0,
    password_hash TEXT NOT NULL DEFAULT '',
    traefik_enabled INTEGER NOT NULL DEFAULT 0,
    traefik_base_domain TEXT NOT NULL DEFAULT '',
    container_id TEXT NOT NULL DEFAULT '',
    container_name TEXT NOT NULL DEFAULT '',
    volume_name TEXT NOT NULL DEFAULT '',
    network_name TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    expires_at TEXT,
    deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_workspaces_state ON workspaces(state);

CREATE TABLE IF NOT EXISTS workspace_ssh_keys (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    key_type TEXT NOT NULL DEFAULT '',
    public_key TEXT NOT NULL,
    comment TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_workspace_ssh_keys_workspace_id ON workspace_ssh_keys(workspace_id);

CREATE TABLE IF NOT EXISTS workspace_events (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    message TEXT NOT NULL,
    detail_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_workspace_events_workspace_id ON workspace_events(workspace_id, created_at DESC);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    job_type TEXT NOT NULL,
    status TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    attempt INTEGER NOT NULL DEFAULT 1,
    latest_log TEXT NOT NULL DEFAULT '',
    cancel_requested INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    run_after TEXT NOT NULL,
    FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_jobs_status_run_after ON jobs(status, run_after);
CREATE INDEX IF NOT EXISTS idx_jobs_workspace_id ON jobs(workspace_id, created_at DESC);

CREATE TABLE IF NOT EXISTS job_logs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    sequence_no INTEGER NOT NULL,
    stream TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_job_logs_job_id ON job_logs(job_id, sequence_no);

CREATE TABLE IF NOT EXISTS port_leases (
    port INTEGER PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    leased_at TEXT NOT NULL,
    released_at TEXT,
    FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS ephemeral_secrets (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    secret_kind TEXT NOT NULL,
    ciphertext TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ephemeral_secrets_workspace_id ON ephemeral_secrets(workspace_id, secret_kind);

