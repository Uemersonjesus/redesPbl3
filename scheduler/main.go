package main

// MangoChain Scheduler — simplified monitor
//
// The dispatch logic has been moved into the ABCI FinalizeBlock:
//   - autoDispatch()    pairs PENDING alerts with IDLE drones every block
//   - checkCompletions() marks missions complete when EndBlock is reached
//   - checkWatchdog()   detects lost drones at WatchdogBlock
//
// This process now only monitors and logs system state for observability.
// It is no longer a single point of failure — the chain self-manages.

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var rpcClient = &http.Client{Timeout: 2 * time.Second}

var (
	abciAPIURL = getEnv("ABCI_API_URL", "http://abci-app:8080")
)

type DroneState struct {
	DroneID        string `json:"drone_id"`
	Status         string `json:"status"`
	CurrentAlertID string `json:"current_alert_id,omitempty"`
}

type AlertState struct {
	AlertID       string  `json:"alert_id"`
	Status        string  `json:"status"`
	AssignedDrone string  `json:"assigned_drone,omitempty"`
	Severity      int     `json:"severity"`
	EndBlock      int64   `json:"end_block,omitempty"`
	WatchdogBlock int64   `json:"watchdog_block,omitempty"`
}

type AppState struct {
	Height int64                      `json:"height"`
	Drones map[string]*DroneState     `json:"drones"`
	Alerts map[string]*AlertState     `json:"alerts"`
}

func main() {
	log.Println("[Scheduler] Starting — waiting 15s for network...")
	time.Sleep(15 * time.Second)
	log.Println("[Scheduler] Online. Beginning dispatch loop.")
	log.Println("[Scheduler] NOTE: dispatch is now handled autonomously by the ABCI FinalizeBlock.")

	heartbeat := time.NewTicker(60 * time.Second)
	defer heartbeat.Stop()

	for range heartbeat.C {
		logHeartbeat()
	}
}

func logHeartbeat() {
	resp, err := rpcClient.Get(abciAPIURL + "/api/state")
	if err != nil {
		log.Printf("[Scheduler] Heartbeat — chain unreachable: %v", err)
		return
	}
	defer resp.Body.Close()

	var state AppState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		log.Printf("[Scheduler] Heartbeat — decode error: %v", err)
		return
	}

	pending, assigned, completed := 0, 0, 0
	for _, a := range state.Alerts {
		switch a.Status {
		case "PENDING":   pending++
		case "ASSIGNED":  assigned++
		case "COMPLETED": completed++
		}
	}

	busy, idle, crashed := 0, 0, 0
	for _, d := range state.Drones {
		switch d.Status {
		case "BUSY":    busy++
		case "IDLE":    idle++
		case "CRASHED", "OFFLINE": crashed++
		}
	}

	log.Printf("[Scheduler] Block=%d | Alerts: %d pending %d active %d done | Drones: %d idle %d busy %d crashed",
		state.Height, pending, assigned, completed, idle, busy, crashed)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}
