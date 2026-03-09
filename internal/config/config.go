package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Addr                  string
	DataDir               string
	DBPath                string
	Runtime               string
	PublicHost            string
	DefaultWorkspaceImage string
	AppSecret             string
	MaxCPUMillis          int
	MaxMemoryMB           int
	MaxTTLMinutes         int
	PortRangeStart        int
	PortRangeEnd          int
	AutoMigrate           bool
	TraefikBaseDomain     string
	DockerBinary          string
	GitBinary             string
	NixpacksBinary        string
	WorkspaceNetwork      string
}

func Load() Config {
	dataDir := getenv("CHEAPSPACE_DATA_DIR", "data")
	dbPath := getenv("CHEAPSPACE_DB_PATH", filepath.Join(dataDir, "cheapspace.db"))

	cfg := Config{
		Addr:                  getenv("CHEAPSPACE_ADDR", "127.0.0.1:8080"),
		DataDir:               dataDir,
		DBPath:                dbPath,
		Runtime:               getenv("CHEAPSPACE_RUNTIME", "mock"),
		PublicHost:            getenv("CHEAPSPACE_PUBLIC_HOST", "localhost"),
		DefaultWorkspaceImage: getenv("CHEAPSPACE_DEFAULT_WORKSPACE_IMAGE", "cheapspace-workspace:latest"),
		AppSecret:             getenv("CHEAPSPACE_APP_SECRET", "cheapspace-development-secret"),
		MaxCPUMillis:          getenvInt("CHEAPSPACE_MAX_CPU_MILLIS", 8000),
		MaxMemoryMB:           getenvInt("CHEAPSPACE_MAX_MEMORY_MB", 16384),
		MaxTTLMinutes:         getenvInt("CHEAPSPACE_MAX_TTL_MINUTES", 1440),
		PortRangeStart:        getenvInt("CHEAPSPACE_PORT_RANGE_START", 2200),
		PortRangeEnd:          getenvInt("CHEAPSPACE_PORT_RANGE_END", 2399),
		AutoMigrate:           getenvBool("CHEAPSPACE_AUTO_MIGRATE", true),
		TraefikBaseDomain:     getenv("CHEAPSPACE_TRAEFIK_BASE_DOMAIN", ""),
		DockerBinary:          getenv("CHEAPSPACE_DOCKER_BINARY", "docker"),
		GitBinary:             getenv("CHEAPSPACE_GIT_BINARY", "git"),
		NixpacksBinary:        getenv("CHEAPSPACE_NIXPACKS_BINARY", "nixpacks"),
		WorkspaceNetwork:      getenv("CHEAPSPACE_WORKSPACE_NETWORK", "bridge"),
	}

	if cfg.PortRangeEnd < cfg.PortRangeStart {
		cfg.PortRangeEnd = cfg.PortRangeStart
	}

	return cfg
}

func (c Config) EncryptionKey() []byte {
	sum := sha256.Sum256([]byte(c.AppSecret))
	out := make([]byte, len(sum))
	copy(out, sum[:])
	return out
}

func (c Config) SecretFingerprint() string {
	sum := sha256.Sum256([]byte(c.AppSecret))
	return hex.EncodeToString(sum[:8])
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func getenvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
