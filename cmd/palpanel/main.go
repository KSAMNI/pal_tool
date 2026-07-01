package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"palpanel-lite/internal/app"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	addr := flag.String("addr", getenv("PALPANEL_ADDR", "127.0.0.1:8080"), "HTTP listen address")
	dataDir := flag.String("data-dir", getenv("PALPANEL_DATA_DIR", "data"), "directory for SQLite database, backups, uploads, and task state")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("PalPanel Lite %s\n", version)
		return nil
	}

	panel, err := app.New(*dataDir)
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}
	defer func() {
		if closeErr := panel.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close app: %w", closeErr)
		}
	}()

	server := &http.Server{
		Addr:    *addr,
		Handler: panel.Routes(),
	}
	serverErr := make(chan error, 1)
	log.Printf("PalPanel Lite %s listening on http://%s", version, *addr)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	case <-signalCtx.Done():
		log.Print("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown server: %w", err)
		}
		if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
