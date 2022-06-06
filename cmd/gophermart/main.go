package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/KokoulinM/go-musthave-diploma-tpl/cmd/gophermart/config"
	"github.com/KokoulinM/go-musthave-diploma-tpl/cmd/gophermart/database"
	"github.com/KokoulinM/go-musthave-diploma-tpl/internal/database/postgres"
	"github.com/KokoulinM/go-musthave-diploma-tpl/internal/handlers"
	"github.com/KokoulinM/go-musthave-diploma-tpl/internal/router"
	"github.com/KokoulinM/go-musthave-diploma-tpl/internal/server"
	"github.com/KokoulinM/go-musthave-diploma-tpl/internal/tasks"
	"github.com/KokoulinM/go-musthave-diploma-tpl/internal/workers"
)

func main() {
	logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)

	logger.Log().Msg("starting server")

	ctx, cancel := context.WithCancel(context.Background())

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	logger.Log().Msg("starting parse configuration")

	cfg := config.New()

	logger.Log().Msg("ServerAddress: " + cfg.ServerAddress)
	logger.Log().Msg("AccrualSystemAddress: " + cfg.AccrualSystemAddress)
	logger.Log().Msg("DataBase: " + cfg.DataBase.DataBaseURI)
	logger.Log().Msg("AccessTokenSecret: " + cfg.Token.AccessTokenSecret)
	logger.Log().Msg("RefreshTokenSecret: " + cfg.Token.RefreshTokenSecret)

	db, err := sql.Open("postgres", cfg.DataBase.DataBaseURI)
	if err != nil {
		logger.Error().Msg(err.Error())
	}

	logger.Log().Msg("finish db connection")

	repo := postgres.New(db)

	jobStore := postgres.NewJobStore(db)
	var listTask []tasks.TaskInterface
	listTask = append(listTask, tasks.NewCheckOrderStatusTask(cfg.AccrualSystemAddress, &logger, repo.ChangeOrderStatus))
	taskStore := tasks.NewTaskStore(listTask)

	wp := workers.New(jobStore, taskStore, &cfg.WorkerPool, &logger)

	go func() {
		wp.Run(ctx)
	}()

	logger.Log().Msg("starting setup db")

	_, err = database.RunMigration(cfg.DataBase.DataBaseURI)
	if err != nil {
		logger.Error().Msg(err.Error())
	}

	logger.Log().Msg("finish setup db")

	handlers := handlers.New(repo, jobStore, &logger, cfg)

	router := router.New(handlers, cfg)

	s := server.New(ctx, router, cfg.ServerAddress)

	go func() error {
		err = s.Start()
		if err != nil {
			return err
		}

		logger.Log().Msgf("httpServer starting at: %s", cfg.ServerAddress)

		return nil
	}()

	select {
	case <-interrupt:
		cancel()
		break
	case <-ctx.Done():
		break
	}

	if err != nil {
		logger.Error().Msgf("server returning an error: %s", err.Error())
		os.Exit(2)
	}
}
