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

type config struct {
	Environment string `env:"ENVIRONMENT" envDefault:"DEVELOPMENT"`
	Database    DatabaseConfig
	Server      ServerConfig
}

func Load() (config, error) {
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return config{}, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}
