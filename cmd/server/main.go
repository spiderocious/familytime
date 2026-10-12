// Command server boots the Family Time API.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"familytime/backend/internal/app"
	"familytime/backend/internal/env"
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/features/results"
	"familytime/backend/internal/features/wallet"
	"familytime/backend/internal/lib/db"
	"familytime/backend/internal/lib/logger"
)

func main() {
	if err := run(); err != nil {
		logger.Error("failed to start server", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := env.Load()
	if err != nil {
		// The logger is configured from env, so this is the one write to stderr.
		fmt.Fprintf(os.Stderr, "Invalid environment configuration: %s\n", err)
		os.Exit(1)
	}
	logger.Init(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The database connects before the port opens: an instance that accepts
	// traffic it cannot serve fails every request instead of failing to start.
	database, err := db.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return fmt.Errorf("mongodb: %w", err)
	}
	logger.Info("mongodb connected", "database", cfg.MongoDB)

	// Warm the question cache before serving. A host pressing Start must never
	// wait on a query, so the whole bank is in memory before the first request.
	questionsRepo := questions.NewRepo(database.DB)
	if err := questionsRepo.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("question indexes: %w", err)
	}
	bank := questions.NewBank()
	started := time.Now()
	if err := bank.Load(ctx, questionsRepo); err != nil {
		return fmt.Errorf("question cache: %w", err)
	}
	logger.Info("question cache warmed", "questions", bank.Total(), "took_ms", time.Since(started).Milliseconds())
	if bank.Total() == 0 {
		logger.Warn("the question bank is empty — run `go run ./cmd/seed` to load it")
	}

	application := app.Build(app.Deps{
		Env:     cfg,
		DB:      database,
		Bank:    bank,
		Results: results.NewService(results.NewRepo(database.DB)),
		Wallets: wallet.NewRepo(database.DB),
	})
	application.Registry.StartSweeper(ctx)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           application.Engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", "port", cfg.Port, "env", cfg.AppEnv, "base_url", fmt.Sprintf("http://localhost:%d/api/v1", cfg.Port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown: stop accepting connections, let in-flight requests
	// finish, then close the database.
	logger.Info("shutdown signal received")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("error while closing http server", "error", err.Error())
	}
	if err := database.Close(shutdownCtx); err != nil {
		logger.Error("error while closing mongodb", "error", err.Error())
	}
	logger.Info("shutdown complete")
	return nil
}
