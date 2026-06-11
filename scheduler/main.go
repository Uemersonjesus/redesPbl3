package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var rpcClient = &http.Client{Timeout: 2 * time.Second}

type TxType string

const (
	TxDroneDispatch TxType = "DRONE_DISPATCH"
	TxMissionReport TxType = "MISSION_REPORT"
	TxDroneStatus   TxType = "DRONE_STATUS"
)

type AlertSeverity int

type Transaction struct {
	ID        string          `json:"id"`
	Type      TxType          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Sender    string          `json:"sender"`
	Payload   json.RawMessage `json:"payload"`
}

type DroneState struct {
	DroneID        string    `json:"drone_id"`
	Status         string    `json:"status"`
	CurrentAlertID string    `json:"current_alert_id,omitempty"`
	LastSeen       time.Time `json:"last_seen"`
}

type AlertPayload struct {
	AlertID  string        `json:"alert_id"`
	SectorID string        `json:"sector_id"`
	SensorID string        `json:"sensor_id"`
	Type     string        `json:"type"`
	Severity AlertSeverity `json:"severity"`
	Location string        `json:"location"`
	Details  string        `json:"details"`
	Status   string        `json:"status"`
}

type AlertState struct {
	AlertPayload
	AssignedDrone string    `json:"assigned_drone,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AppState struct {
	Height int64                   `json:"height"`
	Drones map[string]*DroneState  `json:"drones"`
	Alerts map[string]*AlertState  `json:"alerts"`
}

type DroneDispatchPayload struct {
	DroneID           string    `json:"drone_id"`
	AlertID           string    `json:"alert_id"`
	SectorID          string    `json:"sector_id"`
	DispatchTime      time.Time `json:"dispatch_time"`
	EstimatedDuration int       `json:"estimated_duration_seconds"`
}

type MissionReportPayload struct {
	DroneID   string    `json:"drone_id"`
	AlertID   string    `json:"alert_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Outcome   string    `json:"outcome"`
	Details   string    `json:"details"`
}

type DroneStatusPayload struct {
	DroneID string `json:"drone_id"`
	Status  string `json:"status"`
}

var (
	abciAPIURL = getEnv("ABCI_API_URL", "http://abci-app:8080")
	// cometNodes is the ordered list of CometBFT RPC endpoints.
	// submitTx tries each in order and returns on first success.
	// If node0 is down the scheduler automatically falls back to node1, node2, node3.
	cometNodes = strings.Split(
		getEnv("COMET_RPC_NODES", "http://node0:26657,http://node1:26657,http://node2:26657,http://node3:26657"),
		",",
	)
)

// Mission tracks an in-progress drone assignment (in-memory)
type Mission struct {
	DroneID      string
	AlertID      string
	StartTime    time.Time
	Duration     time.Duration
	// Deadline = StartTime + Duration + grace period
	// If exceeded, drone is presumed lost and alert re-queued
	Deadline     time.Time
}

// Grace period added on top of mission duration before declaring drone lost
const gracePeriod = 30 * time.Second

var activeMissions = map[string]*Mission{}

func main() {
	log.Println("[Scheduler] Starting — waiting 15s for network...")
	time.Sleep(15 * time.Second)
	log.Println("[Scheduler] Online. Beginning dispatch loop.")

	// Main loop: every 5 seconds
	ticker := time.NewTicker(5 * time.Second)
	// Heartbeat log: every 60 seconds to keep terminal readable
	heartbeat := time.NewTicker(60 * time.Second)

	for {
		select {
		case <-ticker.C:
			checkStaleMissions() // watchdog first: free up lost drones
			runScheduler()       // then dispatch pending alerts
			checkMissionCompletions() // complete finished missions
		case <-heartbeat.C:
			logHeartbeat()
		}
	}
}

// checkStaleMissions detects drones that have been on a mission past their
// deadline (e.g. container was killed) and marks them CRASHED on-chain,
// which causes applyDroneStatus to re-queue the associated alert as PENDING.
func checkStaleMissions() {
	now := time.Now()
	for droneID, m := range activeMissions {
		if now.After(m.Deadline) {
			log.Printf("[Scheduler] ⚠  WATCHDOG: drone %s overdue on alert %s (deadline %s ago) — marking CRASHED",
				droneID, m.AlertID, now.Sub(m.Deadline).Round(time.Second))

			// Submit DRONE_STATUS=CRASHED — the ABCI app will re-queue the alert
			status := DroneStatusPayload{DroneID: droneID, Status: "CRASHED"}
			if err := submitTx(TxDroneStatus, status); err != nil {
				log.Printf("[Scheduler] Failed to submit CRASHED status for %s: %v", droneID, err)
				// Don't delete from activeMissions yet — retry next cycle
				continue
			}

			log.Printf("[Scheduler] Drone %s marked CRASHED. Alert %s will be re-queued by chain.", droneID, m.AlertID)
			delete(activeMissions, droneID)
		}
	}
}

func runScheduler() {
	state, err := fetchState()
	if err != nil {
		log.Printf("[Scheduler] Cannot fetch state: %v", err)
		return
	}

	// Collect PENDING alerts sorted by priority (severity DESC, arrival ASC)
	var pending []*AlertState
	for _, a := range state.Alerts {
		if a.Status == "PENDING" {
			pending = append(pending, a)
		}
	}
	if len(pending) == 0 {
		return
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].Severity != pending[j].Severity {
			return pending[i].Severity > pending[j].Severity
		}
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})

	// Collect IDLE drones not already tracked in activeMissions
	var idleDrones []string
	for id, d := range state.Drones {
		if d.Status == "IDLE" {
			if _, tracked := activeMissions[id]; !tracked {
				idleDrones = append(idleDrones, id)
			}
		}
	}
	if len(idleDrones) == 0 {
		return
	}

	log.Printf("[Scheduler] Dispatch cycle: %d pending alerts, %d idle drones", len(pending), len(idleDrones))

	dispatched := 0
	for _, alert := range pending {
		if dispatched >= len(idleDrones) {
			break
		}
		droneID := idleDrones[dispatched]
		dur := missionDuration(alert.Severity)

		dispatch := DroneDispatchPayload{
			DroneID:           droneID,
			AlertID:           alert.AlertID,
			SectorID:          alert.SectorID,
			DispatchTime:      time.Now(),
			EstimatedDuration: int(dur.Seconds()),
		}
		if err := submitTx(TxDroneDispatch, dispatch); err != nil {
			log.Printf("[Scheduler] Dispatch failed (%s → %s): %v", droneID, alert.AlertID, err)
			continue
		}

		activeMissions[droneID] = &Mission{
			DroneID:   droneID,
			AlertID:   alert.AlertID,
			StartTime: time.Now(),
			Duration:  dur,
			Deadline:  time.Now().Add(dur).Add(gracePeriod),
		}
		log.Printf("[Scheduler] ✓ Dispatched %s → alert %s (sev=%d, est=%s, deadline=%s)",
			droneID, alert.AlertID, alert.Severity, dur.Round(time.Second),
			time.Now().Add(dur).Add(gracePeriod).Format("15:04:05"))
		dispatched++
	}
}

func checkMissionCompletions() {
	now := time.Now()
	for droneID, m := range activeMissions {
		// Complete when estimated duration has passed (but not yet at watchdog deadline)
		if now.After(m.StartTime.Add(m.Duration)) && now.Before(m.Deadline) {
			outcome, details := generateOutcome(m.AlertID)
			report := MissionReportPayload{
				DroneID:   droneID,
				AlertID:   m.AlertID,
				StartTime: m.StartTime,
				EndTime:   now,
				Outcome:   outcome,
				Details:   details,
			}
			if err := submitTx(TxMissionReport, report); err != nil {
				log.Printf("[Scheduler] Report submission failed (%s): %v", droneID, err)
				continue
			}
			log.Printf("[Scheduler] ✓ Mission complete: %s finished alert %s → %s",
				droneID, m.AlertID, outcome)
			delete(activeMissions, droneID)
		}
	}
}

func logHeartbeat() {
	if len(activeMissions) == 0 {
		log.Println("[Scheduler] Heartbeat — no active missions")
		return
	}
	log.Printf("[Scheduler] Heartbeat — %d active missions:", len(activeMissions))
	for droneID, m := range activeMissions {
		remaining := time.Until(m.StartTime.Add(m.Duration)).Round(time.Second)
		watchdog := time.Until(m.Deadline).Round(time.Second)
		log.Printf("[Scheduler]   %s → alert %s | remaining=%s | watchdog=%s",
			droneID, m.AlertID, remaining, watchdog)
	}
}

func missionDuration(sev AlertSeverity) time.Duration {
	switch sev {
	case 4:
		return 60 * time.Second
	case 3:
		return 90 * time.Second
	case 2:
		return 120 * time.Second
	default:
		return 150 * time.Second
	}
}

var outcomeOptions = []struct{ outcome, details string }{
	{"ROUTE_CLEAR", "Reconnaissance complete. No obstacles detected on maritime corridor."},
	{"OBSTACLE_DETECTED", "Unidentified floating object detected. Marked for coast guard."},
	{"VESSEL_ASSISTED", "Distressed vessel located and escorted to safe waters."},
	{"SUSPICIOUS_ACTIVITY", "Unusual vessel movement detected. Intel forwarded to consortium."},
	{"FALSE_ALARM", "Alert investigated. Sensor anomaly confirmed. Area secure."},
}

func generateOutcome(alertID string) (string, string) {
	idx := len(alertID) % len(outcomeOptions)
	return outcomeOptions[idx].outcome, outcomeOptions[idx].details
}

func fetchState() (*AppState, error) {
	resp, err := rpcClient.Get(abciAPIURL + "/api/state")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var state AppState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

// submitTx tries each CometBFT node in order until one accepts the transaction.
// This means the scheduler keeps working even if node0 (or any other node) is down.
func submitTx(txType TxType, payload interface{}) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tx := Transaction{
		ID:        uuid.New().String(),
		Type:      txType,
		Timestamp: time.Now(),
		Sender:    "scheduler",
		Payload:   payloadJSON,
	}
	txJSON, err := json.Marshal(tx)
	if err != nil {
		return err
	}

	encoded := base64.StdEncoding.EncodeToString(txJSON)
	reqBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "broadcast_tx_sync",
		"params":  map[string]interface{}{"tx": encoded},
		"id":      1,
	})

	var lastErr error
	for _, nodeURL := range cometNodes {
		nodeURL = strings.TrimSpace(nodeURL)
		if nodeURL == "" {
			continue
		}
		resp, err := rpcClient.Post(nodeURL, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			lastErr = fmt.Errorf("node %s unreachable: %w", nodeURL, err)
			continue // try next node
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if errField, ok := result["error"]; ok && errField != nil {
			lastErr = fmt.Errorf("node %s rpc error: %v", nodeURL, errField)
			continue // try next node
		}
		return nil // success
	}

	return fmt.Errorf("all nodes failed — last error: %w", lastErr)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}
