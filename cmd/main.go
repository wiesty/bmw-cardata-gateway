package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wiesty/bmw-cardata-gateway/internal/api"
	"github.com/wiesty/bmw-cardata-gateway/internal/bmw"
)

const banner = `
  ______
 /|_||_\'.__
(   _    _ _\
='-(_)--(_)-'

Unofficial BMW CarData Gateway
by wiesty

`

func main() {
	fmt.Print(banner)

	clientID := mustEnv("BMW_CLIENT_ID")
	port := envOr("REST_PORT", "8080")
	dataDir := envOr("DATA_DIR", "/data")
	pollMinutes := parseIntOr(envOr("POLL_INTERVAL_MINUTES", "30"), 30)
	if pollMinutes < 10 {
		pollMinutes = 10
	}

	corsOrigins := envOr("CORS_ORIGINS", "*")
	apiKey := os.Getenv("API_KEY")

	// BMW_VINS: optional comma-separated list of VINs to track.
	// If unset, the primary VIN is auto-discovered from the BMW account.
	var explicitVINs []string
	if raw := os.Getenv("BMW_VINS"); raw != "" {
		for _, v := range strings.Split(raw, ",") {
			if vin := strings.ToUpper(strings.TrimSpace(v)); vin != "" {
				explicitVINs = append(explicitVINs, vin)
			}
		}
	}

	sessionPath := filepath.Join(dataDir, "session.json")
	auth := bmw.NewAuth(clientID, sessionPath, func(verificationURI, userCode string) {
		fmt.Printf("\n============================================================\n")
		fmt.Printf("BMW Authentication Required\n")
		fmt.Printf("Visit:  %s\n", verificationURI)
		fmt.Printf("Code:   %s\n", userCode)
		fmt.Printf("Waiting for approval...\n")
		fmt.Printf("============================================================\n\n")
	})

	client := bmw.NewClient(auth)
	ctx := context.Background()

	vins, containerID, err := bmw.BootstrapMulti(ctx, client, dataDir, explicitVINs)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	interval := time.Duration(pollMinutes) * time.Minute
	registry := bmw.NewRegistry()

	for _, vin := range vins {
		cache := bmw.NewCache()
		registry.Add(vin, cache)

		// Per-VIN state file when tracking multiple vehicles; shared state.json for single.
		var statePath string
		if len(vins) > 1 {
			statePath = filepath.Join(dataDir, "state_"+vin+".json")
		} else {
			statePath = filepath.Join(dataDir, "state.json")
		}

		go bmw.StartPoller(ctx, client, vin, containerID, interval, cache, statePath)
	}

	mux := http.NewServeMux()
	api.RegisterHandlers(mux, api.Config{
		CORSOrigins: corsOrigins,
		APIKey:      apiKey,
		Registry:    registry,
	})

	fmt.Printf("  VINs:     %s\n", strings.Join(vins, ", "))
	fmt.Printf("  Endpoint: http://0.0.0.0:%s/vehicle\n", port)
	fmt.Printf("  Poll:     every %d min\n", pollMinutes)
	if apiKey != "" {
		fmt.Printf("  Auth:     API key required (X-API-Key)\n")
	}
	if corsOrigins != "*" {
		fmt.Printf("  CORS:     %s\n", corsOrigins)
	}
	fmt.Println()

	log.Printf("[api] listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseIntOr(s string, fallback int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}
