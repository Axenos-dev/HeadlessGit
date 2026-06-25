package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type DatabaseConfig struct {
	DatabaseURL string `env:"DATABASE_URL,required"`
	AutoMigrate bool   `env:"AUTO_MIGRATE" envDefault:"true"`
}

type ServerConfig struct {
	ControlPort int    `env:"CONTROL_PORT" envDefault:"4001"`
	GitHTTPPort int    `env:"GIT_HTTP_PORT" envDefault:"4000"`
	GitSSHPort  int    `env:"GIT_SSH_PORT" envDefault:"2222"`
	RepoRoot    string `env:"REPO_ROOT" envDefault:"data/repos"`
	HostKeyPath string `env:"SSH_HOST_KEY_PATH" envDefault:"data/ssh/host_ed25519"`
}

type LFSConfig struct {
	Enabled     bool   `env:"LFS_ENABLED" envDefault:"false"`
	StorageType string `env:"LFS_STORAGE_TYPE" envDefault:"disk"` // disk or s3
	Root        string `env:"LFS_ROOT" envDefault:"data/lfs"`     // if storage type is disk
	PublicURL   string `env:"LFS_PUBLIC_URL"`
}

type config struct {
	Environment string `env:"ENVIRONMENT" envDefault:"DEVELOPMENT"`
	Database    DatabaseConfig
	Server      ServerConfig
	LFS         LFSConfig

	// raw token for the seeded admin service account; empty = no admin seeded
	AdminToken string `env:"ADMIN_TOKEN"`
}

func Load() (config, error) {
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return config{}, fmt.Errorf("parse config: %w", err)
	}

	if cfg.LFS.Enabled && cfg.LFS.PublicURL == "" {
		return config{}, fmt.Errorf("LFS_PUBLIC_URL is required when LFS_ENABLED is true")
	}

	return cfg, nil
}
