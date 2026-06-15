package main

import (
	"encoding/json"
	"time"
)

type TxType string

const (
	TxDeposit       TxType = "DEPOSIT"
	TxTransfer      TxType = "TRANSFER"
	TxAlert         TxType = "ALERT"
	TxDroneDispatch TxType = "DRONE_DISPATCH"
	TxMissionReport TxType = "MISSION_REPORT"
	TxDroneStatus   TxType = "DRONE_STATUS"
)

type AlertSeverity int

const (
	SeverityLow      AlertSeverity = 1
	SeverityMedium   AlertSeverity = 2
	SeverityHigh     AlertSeverity = 3
	SeverityCritical AlertSeverity = 4
)

// DispatchCost is the MANGO token cost per drone dispatch, based on severity.
// Deducted from the nation that owns the sector requesting the drone.
func DispatchCost(sev AlertSeverity) float64 {
	switch sev {
	case SeverityCritical:
		return 40.0
	case SeverityHigh:
		return 25.0
	case SeverityMedium:
		return 15.0
	default:
		return 10.0
	}
}

// nationOrder defines the fixed round-robin rotation for dispatch payments.
// When new nations join the consortium, add them here — the index persists
// on-chain in AppState.PaymentIndex so rotation survives restarts.
var nationOrder = []string{"usa", "israel", "iran", "maritimecorp"}

type Transaction struct {
	ID        string          `json:"id"`
	Type      TxType          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Sender    string          `json:"sender"`
	Payload   json.RawMessage `json:"payload"`
	// ED25519 authentication — set by gateway for DEPOSIT and TRANSFER
	PublicKey string          `json:"public_key,omitempty"`
	Signature string          `json:"signature,omitempty"`
}

type DepositPayload struct {
	Nation string  `json:"nation"`
	Fiat   string  `json:"fiat"`
	Amount float64 `json:"amount"`
	Tokens float64 `json:"tokens"`
	Rate   float64 `json:"rate"`
}

type TransferPayload struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Amount float64 `json:"amount"`
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

type DroneDispatchPayload struct {
	DroneID           string    `json:"drone_id"`
	AlertID           string    `json:"alert_id"`
	SectorID          string    `json:"sector_id"`
	DispatchTime      time.Time `json:"dispatch_time"`
	EstimatedDuration int       `json:"estimated_duration_seconds"`
	// Cost is calculated on-chain from alert severity — not trusted from client
}

type MissionReportPayload struct {
	DroneID      string        `json:"drone_id"`
	AlertID      string        `json:"alert_id"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Outcome      string        `json:"outcome"`
	Details      string        `json:"details"`
	// Audit fields — enriched on-chain from alert data
	SectorID     string        `json:"sector_id"`
	SensorID     string        `json:"sensor_id"`
	AlertType    string        `json:"alert_type"`
	Location     string        `json:"location"`
	Severity     AlertSeverity `json:"severity"`
	DispatchCost float64       `json:"dispatch_cost"`
	PaidBy       string        `json:"paid_by"`
}

type DroneStatusPayload struct {
	DroneID string `json:"drone_id"`
	Status  string `json:"status"` // IDLE, BUSY, CRASHED, OFFLINE
}

// ── State ─────────────────────────────────────────────────────────────────────

type NationAccount struct {
	Nation  string  `json:"nation"`
	Balance float64 `json:"balance"`
	Fiat    string  `json:"fiat"`
}

type DroneState struct {
	DroneID        string    `json:"drone_id"`
	Status         string    `json:"status"`
	CurrentAlertID string    `json:"current_alert_id,omitempty"`
	LastSeen       time.Time `json:"last_seen"`
	// CrashedAt is set when status becomes CRASHED, cleared on recovery
	CrashedAt *time.Time `json:"crashed_at,omitempty"`
}

type AlertState struct {
	AlertPayload
	AssignedDrone string    `json:"assigned_drone,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// DispatchCost stores how much was charged when drone was dispatched
	DispatchCost  float64   `json:"dispatch_cost,omitempty"`
	PaidBy        string    `json:"paid_by,omitempty"`
	// Block-based mission timing — set at dispatch, used by FinalizeBlock
	// to complete missions and detect lost drones deterministically.
	// All nodes compute the same values so no external scheduler is needed.
	StartBlock    int64     `json:"start_block,omitempty"`
	EndBlock      int64     `json:"end_block,omitempty"`
	WatchdogBlock int64     `json:"watchdog_block,omitempty"`
}

type AppState struct {
	Height       int64                     `json:"height"`
	Accounts     map[string]*NationAccount `json:"accounts"`
	Drones       map[string]*DroneState    `json:"drones"`
	Alerts       map[string]*AlertState    `json:"alerts"`
	Reports      []MissionReportPayload    `json:"reports"`
	TxHistory    []Transaction             `json:"tx_history"`
	// PaymentIndex is the round-robin cursor — incremented on every successful dispatch.
	// This ensures payment responsibility rotates fairly across all nations,
	// regardless of which sector generated the alert.
	PaymentIndex int                       `json:"payment_index"`
}
