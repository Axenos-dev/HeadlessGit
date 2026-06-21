package main

import (
	"log"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/logger"
	"github.com/Axenos-dev/HeadlessGit/internal/server"
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

	srv := server.NewServer(root, config.Server)
	if err := srv.Run(); err != nil {
		log.Fatal(err)
	}
}
