package main

import (
	"log"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/gitcmd"
	"github.com/Axenos-dev/HeadlessGit/internal/logger"
	"github.com/Axenos-dev/HeadlessGit/internal/server"
	"github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
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

	gitRunner, err := gitcmd.NewRunner(config.Server.RepoRoot)
	if err != nil {
		log.Fatal(err)
	}

	repoService := repositories.NewService(
		root.With(zap.String("service", "repositories")),
		repositories.NewRegistry(db),
		gitRunner,
	)

	srv := server.NewServer(root, config.Server, repoService)
	if err := srv.Run(); err != nil {
		log.Fatal(err)
	}
}
