package main

import (
	"log"
	"net/http"
	"os"

	"palpanel-lite/internal/app"
)

func main() {
	addr := getenv("PALPANEL_ADDR", "127.0.0.1:8080")
	dataDir := getenv("PALPANEL_DATA_DIR", "data")

	panel, err := app.New(dataDir)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}
	defer panel.Close()

	log.Printf("PalPanel Lite listening on http://%s", addr)
	if err := http.ListenAndServe(addr, panel.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
