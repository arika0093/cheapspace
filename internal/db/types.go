package db

import "time"

type Workspace struct {
	ID                  string
	Name                string
	State               string
	RepoURL             string
	DotfilesURL         string
	SourceType          string
	SourceRef           string
	ResolvedImageRef    string
	NixpacksPlanJSON    string
	HTTPProxy           string
	HTTPSProxy          string
	NoProxy             string
	ProxyPACURL         string
	CPUMillis           int
	MemoryMB            int
	TTLMinutes          int
	RuntimeKind         string
	SSHEndpointMode     string
	SSHHost             string
	SSHPort             int
	PublicHostname      string
	PasswordAuthEnabled bool
	PasswordHash        string
	TraefikEnabled      bool
	TraefikBaseDomain   string
	ContainerID         string
	ContainerName       string
	VolumeName          string
	NetworkName         string
	LastError           string
	CreatedAt           time.Time
	StartedAt           *time.Time
	ExpiresAt           *time.Time
	DeletedAt           *time.Time
}

type WorkspaceSSHKey struct {
	ID          string
	WorkspaceID string
	KeyType     string
	PublicKey   string
	Comment     string
	CreatedAt   time.Time
}

type WorkspaceEvent struct {
	ID          string
	WorkspaceID string
	EventType   string
	Message     string
	DetailJSON  string
	CreatedAt   time.Time
}

type Job struct {
	ID              string
	WorkspaceID     string
	JobType         string
	Status          string
	PayloadJSON     string
	Attempt         int
	LatestLog       string
	CancelRequested bool
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	RunAfter        time.Time
}

type JobLog struct {
	ID        string
	JobID     string
	Sequence  int
	Stream    string
	Message   string
	CreatedAt time.Time
}

type EphemeralSecret struct {
	ID          string
	WorkspaceID string
	SecretKind  string
	Ciphertext  string
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	CreatedAt   time.Time
}

type MigrationStatus struct {
	Version   string
	Applied   bool
	AppliedAt *time.Time
}
