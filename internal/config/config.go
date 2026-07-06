package config

import (
	"fmt"
	"strings"
	"time"

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

	// background maintenance,
	// a zero duration disables the loop
	TokenGCInterval time.Duration `env:"TOKEN_GC_INTERVAL" envDefault:"1h"`
	RepoGCInterval  time.Duration `env:"REPO_GC_INTERVAL" envDefault:"5h"`
	WebhookWorkers  int           `env:"WEBHOOK_WORKERS" envDefault:"3"`
}

type LFSConfig struct {
	Enabled     bool   `env:"LFS_ENABLED" envDefault:"false"`
	StorageType string `env:"LFS_STORAGE_TYPE" envDefault:"disk"` // disk or s3
	Root        string `env:"LFS_ROOT" envDefault:"data/lfs"`     // if storage type is disk
	PublicURL   string `env:"LFS_PUBLIC_URL"`

	S3 S3Config
}

type S3Config struct {
	Bucket       string `env:"LFS_S3_BUCKET"`
	Region       string `env:"LFS_S3_REGION"` // e.g. "us-east-1" for AWS, "auto" for R2
	Endpoint     string `env:"LFS_S3_ENDPOINT"`
	AccessKey    string `env:"LFS_S3_ACCESS_KEY_ID"`
	SecretKey    string `env:"LFS_S3_SECRET_ACCESS_KEY"`
	UseSSL       bool   `env:"LFS_S3_USE_SSL" envDefault:"true"`
	UsePathStyle bool   `env:"LFS_S3_USE_PATH_STYLE" envDefault:"false"`
	KeyPrefix    string `env:"LFS_S3_KEY_PREFIX"` // optional prefix for all object keys
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

	if cfg.LFS.Enabled {
		if cfg.LFS.PublicURL == "" {
			return config{}, fmt.Errorf("LFS_PUBLIC_URL is required when LFS_ENABLED is true")
		}
		if cfg.LFS.StorageType == "s3" {
			if err := cfg.LFS.S3.validate(); err != nil {
				return config{}, err
			}
		}
	}

	return cfg, nil
}

func (c S3Config) validate() error {
	missing := []string{}
	if c.Bucket == "" {
		missing = append(missing, "LFS_S3_BUCKET")
	}
	if c.Endpoint == "" {
		missing = append(missing, "LFS_S3_ENDPOINT")
	}
	if c.AccessKey == "" {
		missing = append(missing, "LFS_S3_ACCESS_KEY_ID")
	}
	if c.SecretKey == "" {
		missing = append(missing, "LFS_S3_SECRET_ACCESS_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("LFS_STORAGE_TYPE=s3 requires: %s", strings.Join(missing, ", "))
	}
	return nil
}
