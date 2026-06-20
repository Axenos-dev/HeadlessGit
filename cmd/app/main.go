package app

import (
	"log"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/logger"
	"github.com/Axenos-dev/HeadlessGit/internal/server"
	"go.uber.org/zap"
)

func main() {
	config, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	logger, err := logger.New()
	if err != nil {
		log.Fatal(err)
	}
	defer logger.Sync()

	root := logger.With(
		zap.String("environment", config.Environment),
	)

	srv := server.NewServer(root)
	if err := srv.Run(config.Server); err != nil {
		log.Fatal(err)
	}
}
