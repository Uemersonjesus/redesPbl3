package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	abciserver "github.com/cometbft/cometbft/abci/server"
	cmtlog "github.com/cometbft/cometbft/libs/log"
)

func main() {
	abciAddr := getEnv("ABCI_ADDR", "tcp://0.0.0.0:26658")
	apiPort  := getEnv("API_PORT", "8080")
	statePath := getEnv("STATE_PATH", "/data/state.json")

	app := NewMangoApp(statePath)

	// CometBFT v0.38: NewServer(addr, transport, app)
	srv, err := abciserver.NewServer(abciAddr, "socket", app)
	if err != nil {
		log.Fatalf("Failed to create ABCI server: %v", err)
	}
	srv.SetLogger(cmtlog.NewTMLogger(cmtlog.NewSyncWriter(os.Stdout)))
	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start ABCI server: %v", err)
	}
	log.Printf("[MangoApp] ABCI socket listening on %s", abciAddr)

	// HTTP API for frontends
	mux := http.NewServeMux()
	setupRoutes(mux, app)
	log.Printf("[MangoApp] HTTP API listening on :%s", apiPort)
	if err := http.ListenAndServe(":"+apiPort, corsMiddleware(mux)); err != nil {
		log.Fatalf("HTTP server: %v", err)
	}
}

func setupRoutes(mux *http.ServeMux, app *MangoApp) {
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, app.GetState())
	})
	mux.HandleFunc("/api/accounts", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, app.GetState().Accounts)
	})
	mux.HandleFunc("/api/drones", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, app.GetState().Drones)
	})
	mux.HandleFunc("/api/alerts", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, app.GetState().Alerts)
	})
	mux.HandleFunc("/api/reports", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, app.GetState().Reports)
	})
	mux.HandleFunc("/api/txhistory", func(w http.ResponseWriter, r *http.Request) {
		h := app.GetState().TxHistory
		if len(h) > 50 {
			h = h[len(h)-50:]
		}
		jsonResp(w, h)
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, map[string]interface{}{
			"status": "ok",
			"time":   time.Now(),
			"height": app.GetState().Height,
		})
	})
}

func jsonResp(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}
