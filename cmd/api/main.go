// Command api serve o modelo do andar para o frontend 3D.
//
// Variáveis de ambiente:
//
//	DESKMAP_ADDR          endereço HTTP (padrão ":8090")
//	DESKMAP_DATA_DIR      pasta com floors/<id>/layout.json (padrão "data")
//	DESKMAP_CORS_ORIGINS  origens liberadas, separadas por vírgula
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

	"github.com/Gabrielkr28/company-desk-room-schedule-backend/internal/httpapi"
	"github.com/Gabrielkr28/company-desk-room-schedule-backend/internal/storage"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("API encerrada com erro", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	addr := env("DESKMAP_ADDR", ":8090")
	dataDir := env("DESKMAP_DATA_DIR", "data")
	origins := strings.Split(env("DESKMAP_CORS_ORIGINS", "http://localhost:5190,http://127.0.0.1:5190"), ",")

	store, err := storage.NewFileStore(dataDir, logger)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(store, logger, origins).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("DeskMap API no ar", "addr", addr, "data", dataDir)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logger.Info("encerrando")
	return srv.Shutdown(shutdownCtx)
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
