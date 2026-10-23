package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/wiesty/bmw-cardata-gateway/internal/bmw"
)

// Config holds runtime configuration for the HTTP handlers.
type Config struct {
	// CORSOrigins is "*" (allow all) or a comma-separated list of allowed origins.
	// Defaults to "*" if empty.
	CORSOrigins string
	// APIKey is the required value of the X-API-Key header.
	// If empty, no authentication is required.
	APIKey string
	// Registry holds all tracked vehicles. For single-vehicle mode it contains one entry.
	Registry *bmw.VehicleRegistry
}

func corsMiddleware(origins string) func(http.HandlerFunc) http.HandlerFunc {
	allowAll := origins == "" || origins == "*"
	allowed := make(map[string]bool)
	if !allowAll {
		for _, o := range strings.Split(origins, ",") {
			allowed[strings.TrimSpace(o)] = true
		}
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" && allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next(w, r)
		}
	}
}

func apiKeyMiddleware(key string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if key == "" {
				next(w, r)
				return
			}
			provided := r.Header.Get("X-API-Key")
			if provided == "" {
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					provided = strings.TrimPrefix(auth, "Bearer ")
				}
			}
			if provided != key {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}
			next(w, r)
		}
	}
}

func RegisterHandlers(mux *http.ServeMux, cfg Config) {
	cors := corsMiddleware(cfg.CORSOrigins)
	auth := apiKeyMiddleware(cfg.APIKey)

	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		return cors(auth(h))
	}

	mux.HandleFunc("/health", wrap(healthHandler(cfg.Registry)))
	mux.HandleFunc("/vehicles", wrap(vehiclesHandler(cfg.Registry)))
	mux.HandleFunc("/vehicle/", wrap(vehicleByVINHandler(cfg.Registry)))
	mux.HandleFunc("/vehicle", wrap(vehicleHandler(cfg.Registry)))
	mux.HandleFunc("/openapi.json", cors(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(openapiSpec))
	}))
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(swaggerUI))
	})
}

func healthHandler(reg *bmw.VehicleRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		vins := reg.VINs()
		if len(vins) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{"status": "starting", "last_update": nil})
			return
		}

		// Report overall health: ok only if all vehicles have data.
		var lastUpdate *string
		allReady := true
		for _, vin := range vins {
			data := reg.Get(vin).Get()
			if data == nil {
				allReady = false
				continue
			}
			t := data.LastUpdate.Format(time.RFC3339)
			if lastUpdate == nil || t > *lastUpdate {
				lastUpdate = &t
			}
		}

		if !allReady {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]any{"status": "starting", "last_update": lastUpdate})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "last_update": lastUpdate})
	}
}

// vehiclesHandler lists all tracked VINs and their last update time.
func vehiclesHandler(reg *bmw.VehicleRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		type entry struct {
			VIN        string  `json:"vin"`
			LastUpdate *string `json:"last_update"`
			Ready      bool    `json:"ready"`
		}

		vins := reg.VINs()
		result := make([]entry, 0, len(vins))
		for _, vin := range vins {
			e := entry{VIN: vin}
			if data := reg.Get(vin).Get(); data != nil {
				t := data.LastUpdate.Format(time.RFC3339)
				e.LastUpdate = &t
				e.Ready = true
			}
			result = append(result, e)
		}
		json.NewEncoder(w).Encode(result)
	}
}

// vehicleByVINHandler handles GET /vehicle/{vin}.
func vehicleByVINHandler(reg *bmw.VehicleRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		vin := strings.TrimPrefix(r.URL.Path, "/vehicle/")
		vin = strings.ToUpper(strings.TrimSpace(vin))
		if vin == "" {
			http.Error(w, "missing VIN", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		cache := reg.Get(vin)
		if cache == nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "VIN not tracked: " + vin})
			return
		}
		data := cache.Get()
		if data == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "no data available yet"})
			return
		}
		json.NewEncoder(w).Encode(data)
	}
}

// vehicleHandler handles GET /vehicle for backward compatibility.
// With a single vehicle it returns that vehicle's data.
// With multiple vehicles it returns 400 and instructs to use /vehicle/{vin}.
func vehicleHandler(reg *bmw.VehicleRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		vins := reg.VINs()
		if len(vins) > 1 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "multiple vehicles tracked — use /vehicle/{vin} or /vehicles",
			})
			return
		}
		if len(vins) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "no data available yet"})
			return
		}

		data := reg.Get(vins[0]).Get()
		if data == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "no data available yet"})
			return
		}
		json.NewEncoder(w).Encode(data)
	}
}
