package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// rpcClient has a short timeout so node fallback is fast
var rpcClient = &http.Client{Timeout: 2 * time.Second}

type TxType string

const TxDroneStatus TxType = "DRONE_STATUS"

type Transaction struct {
	ID        string          `json:"id"`
	Type      TxType          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Sender    string          `json:"sender"`
	Payload   json.RawMessage `json:"payload"`
}

type DroneStatusPayload struct {
	DroneID string `json:"drone_id"`
	Status  string `json:"status"`
}

type DroneState struct {
	DroneID        string    `json:"drone_id"`
	Status         string    `json:"status"`
	CurrentAlertID string    `json:"current_alert_id,omitempty"`
	LastSeen       time.Time `json:"last_seen"`
}

var (
	droneID    = getEnv("DRONE_ID", "drone-1")
	abciAPIURL = getEnv("ABCI_API_URL", "http://abci-app:8080")
	cometNodes = strings.Split(
		getEnv("COMET_RPC_NODES", "http://node0:26657,http://node1:26657,http://node2:26657,http://node3:26657"),
		",",
	)
	crashProb = 0.005
)

func main() {
	// ── Graceful shutdown: catch SIGTERM/SIGINT (docker stop sends SIGTERM) ──
	// When the signal arrives we report OFFLINE immediately before the process
	// dies, so the dashboard and scheduler see the change right away instead of
	// waiting for the watchdog timeout.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	log.Printf("[Drone %s] Container started — checking chain state...", droneID)
	time.Sleep(15 * time.Second)

	// ── Startup: query chain to decide what status to report ─────────────────
	chainStatus := queryChainStatus()
	log.Printf("[Drone %s] Chain reports status: %s", droneID, chainStatus)

	switch chainStatus {
	case "CRASHED":
		log.Printf("[Drone %s] Chain shows CRASHED — waiting 2min recovery cooldown", droneID)
		select {
		case <-time.After(2 * time.Minute):
		case <-ctx.Done():
			log.Printf("[Drone %s] Shutdown during recovery cooldown — reporting OFFLINE", droneID)
			reportStatus("OFFLINE")
			return
		}
		reportStatus("IDLE")
		log.Printf("[Drone %s] ✓ Recovery complete — now IDLE", droneID)

	case "BUSY":
		// Restarted mid-mission — watchdog will resolve via timeout; do nothing.
		log.Printf("[Drone %s] Chain shows BUSY — watchdog will resolve.", droneID)

	default: // IDLE, OFFLINE, UNKNOWN
		reportStatus("IDLE")
		log.Printf("[Drone %s] ✓ Online and IDLE", droneID)
	}

	// ── Main heartbeat loop ───────────────────────────────────────────────────
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	simulatedCrash := false
	cycleCount := 0

	for {
		select {

		// ── Shutdown signal received ──────────────────────────────────────────
		case <-ctx.Done():
			log.Printf("[Drone %s] 🔴 SIGTERM received — reporting OFFLINE to chain", droneID)
			reportStatus("OFFLINE")
			log.Printf("[Drone %s] Goodbye.", droneID)
			return

		// ── Heartbeat tick ────────────────────────────────────────────────────
		case <-heartbeat.C:
			cycleCount++

			if simulatedCrash {
				log.Printf("[Drone %s] Recovering from simulated crash...", droneID)
				select {
				case <-time.After(2 * time.Minute):
				case <-ctx.Done():
					log.Printf("[Drone %s] Shutdown during simulated crash recovery — reporting OFFLINE", droneID)
					reportStatus("OFFLINE")
					return
				}
				simulatedCrash = false
				reportStatus("IDLE")
				log.Printf("[Drone %s] ✓ Back online after simulated crash", droneID)
				continue
			}

			// Random simulated crash (very rare)
			if rng.Float64() < crashProb {
				log.Printf("[Drone %s] 💥 SIMULATED CRASH — reporting CRASHED to chain", droneID)
				reportStatus("CRASHED")
				simulatedCrash = true
				continue
			}

			// Periodic log every ~5 min
			if cycleCount%15 == 0 {
				log.Printf("[Drone %s] ✓ Operational (cycle %d)", droneID, cycleCount)
			}
		}
	}
}

// queryChainStatus fetches this drone's current status from the ABCI HTTP API.
// Returns "UNKNOWN" on any error — safe default, will report IDLE on startup.
func queryChainStatus() string {
	resp, err := rpcClient.Get(abciAPIURL + "/api/drones")
	if err != nil {
		log.Printf("[Drone %s] Could not reach chain API: %v — assuming UNKNOWN", droneID, err)
		return "UNKNOWN"
	}
	defer resp.Body.Close()

	var drones map[string]*DroneState
	if err := json.NewDecoder(resp.Body).Decode(&drones); err != nil {
		log.Printf("[Drone %s] Could not decode drones response: %v", droneID, err)
		return "UNKNOWN"
	}

	d, ok := drones[droneID]
	if !ok {
		return "UNKNOWN" // first boot
	}
	return d.Status
}

func reportStatus(status string) {
	payload := DroneStatusPayload{DroneID: droneID, Status: status}
	payloadJSON, _ := json.Marshal(payload)
	tx := Transaction{
		ID:        uuid.New().String(),
		Type:      TxDroneStatus,
		Timestamp: time.Now(),
		Sender:    droneID,
		Payload:   payloadJSON,
	}
	txJSON, _ := json.Marshal(tx)

	encoded := base64.StdEncoding.EncodeToString(txJSON)
	reqBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "broadcast_tx_sync",
		"params":  map[string]interface{}{"tx": encoded},
		"id":      1,
	})

	for _, nodeURL := range cometNodes {
		nodeURL = strings.TrimSpace(nodeURL)
		if nodeURL == "" {
			continue
		}
		resp, err := rpcClient.Post(nodeURL, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			log.Printf("[Drone %s] Node %s unreachable, trying next...", droneID, nodeURL)
			continue
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if errField, ok := result["error"]; ok && errField != nil {
			log.Printf("[Drone %s] ✗ Chain rejected %s via %s: %v", droneID, status, nodeURL, errField)
			continue
		}
		log.Printf("[Drone %s] ✓ Reported status: %s (via %s)", droneID, status, nodeURL)
		return
	}
	log.Printf("[Drone %s] ✗ All nodes unreachable — could not report %s", droneID, status)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}
