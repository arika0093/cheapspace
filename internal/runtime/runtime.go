package runtime

import (
	"context"
	"errors"
)

var ErrCancelled = errors.New("job cancelled")

type Logger interface {
	Log(stream, message string)
}

type Control interface {
	Cancelled(ctx context.Context) bool
}

type BuildRequest struct {
	WorkspaceID    string
	Name           string
	SourceType     string
	SourceRef      string
	RepoURL        string
	DotfilesURL    string
	DefaultImage   string
	PublicHost     string
	PublicHostname string
}

type BuildResult struct {
	ResolvedImageRef string
	NixpacksPlanJSON string
}

type ProvisionRequest struct {
	WorkspaceID         string
	Name                string
	SourceType          string
	SourceRef           string
	ResolvedImageRef    string
	RepoURL             string
	DotfilesURL         string
	AuthorizedKeys      []string
	Password            string
	PasswordAuthEnabled bool
	HTTPProxy           string
	HTTPSProxy          string
	NoProxy             string
	ProxyPACURL         string
	CPUMillis           int
	MemoryMB            int
	PublicHost          string
	PublicHostname      string
	SSHPort             int
	TraefikEnabled      bool
	TraefikBaseDomain   string
	WorkspaceNetwork    string
}

type ProvisionResult struct {
	ContainerID    string
	ContainerName  string
	VolumeName     string
	NetworkName    string
	SSHHost        string
	SSHPort        int
	PublicHostname string
}

type DeleteRequest struct {
	WorkspaceID   string
	ContainerID   string
	ContainerName string
	VolumeName    string
	NetworkName   string
}

type Runtime interface {
	Kind() string
	Build(ctx context.Context, req BuildRequest, control Control, logger Logger) (BuildResult, error)
	Provision(ctx context.Context, req ProvisionRequest, control Control, logger Logger) (ProvisionResult, error)
	Delete(ctx context.Context, req DeleteRequest, control Control, logger Logger) error
}

type NoopControl struct{}

func (NoopControl) Cancelled(context.Context) bool {
	return false
}
