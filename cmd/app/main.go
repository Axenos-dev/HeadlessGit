package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	"github.com/Axenos-dev/HeadlessGit/internal/logger"
	"github.com/Axenos-dev/HeadlessGit/internal/server"
	"github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	"github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	"github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	"github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"github.com/Axenos-dev/HeadlessGit/internal/storage"
	"go.uber.org/zap"
)

func main() {
	config, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := db.Open(config.Database.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if config.Database.AutoMigrate {
		if err := db.Migrate(); err != nil {
			log.Fatal(err)
		}
	}

	logger, err := logger.New()
	if err != nil {
		log.Fatal(err)
	}
	defer logger.Sync()

	root := logger.With(
		zap.String("environment", config.Environment),
	)

	gitBackend, err := gitbackend.NewLocal(config.Server.RepoRoot)
	if err != nil {
		log.Fatal(err)
	}

	repoService := repositories.NewService(
		root.With(zap.String("service", "repositories")),
		repositories.NewRegistry(db),
		gitBackend,
	)

	authService := auth.NewService(
		root.With(zap.String("service", "auth")),
		auth.NewRegistry(db),
	)

	if config.AdminToken != "" {
		if err := authService.SeedAdmin(context.Background(), config.AdminToken); err != nil {
			log.Fatal(err)
		}
	}

	permsService := permissions.NewService(permissions.NewRegistry(db))
	usersService := users.NewService(users.NewRegistry(db))

	// nil when LFS is disabled
	var lfsService *lfs.Service
	if config.LFS.Enabled {
		// resolve the LFS storage, could be or disk or S3
		store, err := newLFSStorage(config.LFS)
		if err != nil {
			log.Fatal(err)
		}

		// and initialize the service
		lfsService = lfs.NewService(
			root.With(zap.String("service", "lfs")),
			lfs.NewRegistry(db),
			store,
			config.LFS.PublicURL,
		)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	srv := server.NewServer(root, config.Server, server.Services{
		Repositories:   repoService,
		Users:          usersService,
		Authentication: authService,
		Authorization:  permsService,
		GitBackend:     gitBackend,
		LFS:            lfsService,
		DB:             db,
	})
	if err := srv.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func newLFSStorage(cfg config.LFSConfig) (lfs.ObjectStorage, error) {
	switch cfg.StorageType {
	case "disk":
		return storage.NewDisk(cfg.Root)
	case "s3":
		return storage.NewS3(cfg.S3)
	default:
		return nil, fmt.Errorf("unknown lfs storage type %q", cfg.StorageType)
	}
}
