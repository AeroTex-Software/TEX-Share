package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AeroTex-Software/TEX-Share/internal/app"
	"github.com/AeroTex-Software/TEX-Share/internal/config"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		cfg, _ := config.Load()
		if cfg != nil {
			os.Stdout.WriteString("TEX Share " + cfg.Version + " (" + cfg.Commit + ")\n")
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		os.Stderr.WriteString("configuration error: " + err.Error() + "\n")
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel(cfg.LogLevel)}))
	slog.SetDefault(logger)

	if cfg.PublicAddr == cfg.AdminAddr {
		logger.Error("public and admin addresses must differ")
		os.Exit(1)
	}
	application, err := app.New(cfg, logger)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer application.Close()

	publicServer := &http.Server{
		Addr:              cfg.PublicAddr,
		Handler:           application.PublicHandler(),
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	adminServer := &http.Server{
		Addr:              cfg.AdminAddr,
		Handler:           application.AdminHandler(),
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 2)
	go func() {
		logger.Info("public server listening", "addr", cfg.PublicAddr)
		if err := publicServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		logger.Info("admin server listening", "addr", cfg.AdminAddr)
		if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-stop:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		logger.Error("server failed", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = publicServer.Shutdown(ctx)
	_ = adminServer.Shutdown(ctx)
	if err := application.Close(); err != nil {
		logger.Warn("shutdown error", "error", err)
	}
	logger.Info("stopped")
}

func logLevel(value string) slog.Level {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
