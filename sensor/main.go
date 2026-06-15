package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

var rpcClient = &http.Client{Timeout: 2 * time.Second}

type TxType string

const TxAlert TxType = "ALERT"

type AlertSeverity int

type Transaction struct {
	ID        string          `json:"id"`
	Type      TxType          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Sender    string          `json:"sender"`
	Payload   json.RawMessage `json:"payload"`
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

var (
	sensorID    = getEnv("SENSOR_ID", "sensor-1")
	sectorID    = getEnv("SECTOR_ID", "sector-alpha")
	cometNodes  = strings.Split(
		getEnv("COMET_RPC_NODES", "http://node0:26657,http://node1:26657,http://node2:26657,http://node3:26657"),
		",",
	)
	minInterval = 45 * time.Second
	maxInterval = 90 * time.Second
)

var alertTypes = []struct {
	alertType string
	severity  AlertSeverity
	details   []string
}{
	{"ROUTE_BLOCK", 4, []string{
		"Radar indicates potential route blockage at northern corridor.",
		"Vessel convoy reports obstruction at shipping lane 7.",
	}},
	{"VESSEL_ADRIFT", 3, []string{
		"Unresponsive vessel detected drifting in maritime exclusion zone.",
		"Small cargo vessel showing no propulsion signals near buoy-14.",
	}},
	{"UNIDENTIFIED_OBJECT", 3, []string{
		"Sonar detected unknown submerged object at maritime grid F-7.",
		"Unidentified floating debris field blocking commercial lane.",
	}},
	{"SIGNAL_FAILURE", 2, []string{
		"Coastal radar buoy-22 has gone silent. Possible damage.",
		"Navigation beacon failure detected at waypoint Omega-3.",
	}},
	{"CONGESTION", 2, []string{
		"High vessel density detected in commercial corridor sector B.",
		"Traffic congestion in maritime lane 4 exceeding safe capacity.",
	}},
	{"ENVIRONMENTAL_RISK", 1, []string{
		"Oil slick detected. Rerouting advisory recommended.",
		"Weather anomaly creating hazardous conditions in sector delta.",
	}},
}

var locations = []string{
	"Grid-A4 (26.5°N 56.2°E)", "Grid-B7 (26.8°N 56.5°E)",
	"Grid-C2 (27.1°N 57.0°E)", "Grid-D5 (26.3°N 55.8°E)",
	"Grid-E9 (27.4°N 56.9°E)", "Grid-F1 (26.0°N 55.5°E)",
}

func main() {
	log.Printf("[Sensor %s] Starting in sector %s...", sensorID, sectorID)
	time.Sleep(20 * time.Second) // let network stabilize

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for {
		interval := minInterval + time.Duration(rng.Int63n(int64(maxInterval-minInterval)))
		time.Sleep(interval)

		alert := generateAlert(rng)
		if err := submitAlert(alert); err != nil {
			log.Printf("[Sensor %s] ✗ Alert failed: %v", sensorID, err)
		} else {
			log.Printf("[Sensor %s] ⚡ %s | %s | sev=%d | %s",
				sensorID, alert.AlertID, alert.Type, alert.Severity, alert.Location)
		}
	}
}

func generateAlert(rng *rand.Rand) AlertPayload {
	at := alertTypes[rng.Intn(len(alertTypes))]
	details := at.details[rng.Intn(len(at.details))]
	return AlertPayload{
		AlertID:  "ALT-" + uuid.New().String()[:8],
		SectorID: sectorID,
		SensorID: sensorID,
		Type:     at.alertType,
		Severity: at.severity,
		Location: locations[rng.Intn(len(locations))],
		Details:  details,
		Status:   "PENDING",
	}
}

func submitAlert(alert AlertPayload) error {
	payloadJSON, err := json.Marshal(alert)
	if err != nil {
		return err
	}
	tx := Transaction{
		ID:        uuid.New().String(),
		Type:      TxAlert,
		Timestamp: time.Now(),
		Sender:    sensorID,
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
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if errField, ok := result["error"]; ok && errField != nil {
			lastErr = fmt.Errorf("rpc error: %v", errField)
			continue
		}
		return nil
	}
	return fmt.Errorf("all nodes failed: %w", lastErr)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}
