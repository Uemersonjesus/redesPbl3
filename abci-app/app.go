package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	abcitypes "github.com/cometbft/cometbft/abci/types"
)

type MangoApp struct {
	mu            sync.RWMutex
	state         *AppState
	statePath     string
	exchangeRates map[string]float64
}

var _ abcitypes.Application = (*MangoApp)(nil)

func NewMangoApp(statePath string) *MangoApp {
	app := &MangoApp{
		statePath: statePath,
		exchangeRates: map[string]float64{
			"USD": 1.0,
			"ILS": 0.27,
			"IRR": 0.000024,
			"EUR": 1.08,
		},
	}
	app.state = app.loadOrInitState()
	return app
}

func (app *MangoApp) loadOrInitState() *AppState {
	if app.statePath != "" {
		data, err := os.ReadFile(app.statePath)
		if err == nil {
			var s AppState
			if err := json.Unmarshal(data, &s); err == nil {
				log.Printf("[MangoApp] Loaded state from disk: height=%d alerts=%d reports=%d",
					s.Height, len(s.Alerts), len(s.Reports))
				return &s
			}
		}
	}
	log.Println("[MangoApp] No state on disk — initializing genesis state")
	return app.genesisState()
}

func (app *MangoApp) genesisState() *AppState {
	return &AppState{
		Height: 0,
		Accounts: map[string]*NationAccount{
			"usa":          {Nation: "usa", Balance: 0, Fiat: "USD"},
			"israel":       {Nation: "israel", Balance: 0, Fiat: "ILS"},
			"iran":         {Nation: "iran", Balance: 0, Fiat: "IRR"},
			"maritimecorp": {Nation: "maritimecorp", Balance: 0, Fiat: "EUR"},
		},
		Drones: map[string]*DroneState{
			"drone-1": {DroneID: "drone-1", Status: "IDLE", LastSeen: time.Now()},
			"drone-2": {DroneID: "drone-2", Status: "IDLE", LastSeen: time.Now()},
			"drone-3": {DroneID: "drone-3", Status: "IDLE", LastSeen: time.Now()},
		},
		Alerts:       map[string]*AlertState{},
		Reports:      []MissionReportPayload{},
		TxHistory:    []Transaction{},
		PaymentIndex: 0,
	}
}

func (app *MangoApp) saveState() {
	if app.statePath == "" {
		return
	}
	data, err := json.Marshal(app.state)
	if err != nil {
		return
	}
	tmp := app.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, app.statePath)
}

// ── Info ──────────────────────────────────────────────────────────────────────

func (app *MangoApp) Info(_ context.Context, _ *abcitypes.RequestInfo) (*abcitypes.ResponseInfo, error) {
	app.mu.RLock()
	defer app.mu.RUnlock()
	log.Printf("[MangoApp] Info called — reporting height=%d to CometBFT", app.state.Height)
	return &abcitypes.ResponseInfo{
		Data:            "MangoChain",
		AppVersion:      1,
		LastBlockHeight: app.state.Height,
	}, nil
}

// ── InitChain ─────────────────────────────────────────────────────────────────

func (app *MangoApp) InitChain(_ context.Context, _ *abcitypes.RequestInitChain) (*abcitypes.ResponseInitChain, error) {
	log.Println("[MangoApp] Chain initialized")
	return &abcitypes.ResponseInitChain{}, nil
}

// ── CheckTx ───────────────────────────────────────────────────────────────────

// nationPublicKeys contains the known ED25519 public keys for each nation gateway.
// These match the private keys hardcoded in the gateway service.
// Only TXs from gateways (DEPOSIT, TRANSFER) require signature verification.
// Internal TXs (ALERT, DRONE_DISPATCH, MISSION_REPORT, DRONE_STATUS) are
// submitted by sensors/drones/abci which do not have nation keys.
var nationPublicKeys = map[string]string{
	"usa":          "2be0b6bef731705e674abd0b587cce435753bc2117022d76031dd2675b679bf6",
	"israel":       "5cc951f22cb034c952b03d6a071021602e2aa647323364cb79ae8053cc456283",
	"iran":         "3f4831ba73c5906e0f890fb685abcc21ca7a550161f98ca65830a393b5b2e976",
	"maritimecorp": "ef436e381cef55df13a59166cd58cca1c08937abcf8285fe6b53c22c7b4e172a",
}

func (app *MangoApp) CheckTx(_ context.Context, req *abcitypes.RequestCheckTx) (*abcitypes.ResponseCheckTx, error) {
	var tx Transaction
	if err := json.Unmarshal(req.Tx, &tx); err != nil {
		return &abcitypes.ResponseCheckTx{Code: 1, Log: "invalid json"}, nil
	}
	if tx.ID == "" || tx.Type == "" {
		return &abcitypes.ResponseCheckTx{Code: 1, Log: "missing id or type"}, nil
	}

	// Verify ED25519 signature for gateway transactions (DEPOSIT, TRANSFER).
	// Sensor, drone and internal transactions do not carry nation signatures.
	if tx.Type == TxDeposit || tx.Type == TxTransfer {
		if tx.Signature == "" || tx.PublicKey == "" {
			return &abcitypes.ResponseCheckTx{Code: 1, Log: "missing signature — gateway txs must be signed"}, nil
		}
		// Verify that the public key matches the known key for this sender nation
		knownPubHex, known := nationPublicKeys[tx.Sender]
		if !known {
			return &abcitypes.ResponseCheckTx{Code: 1, Log: "unknown sender nation: " + tx.Sender}, nil
		}
		if tx.PublicKey != knownPubHex {
			return &abcitypes.ResponseCheckTx{Code: 1, Log: "public key mismatch for nation " + tx.Sender}, nil
		}
		// Verify the signature against the payload
		pubKeyBytes, err := hex.DecodeString(tx.PublicKey)
		if err != nil {
			return &abcitypes.ResponseCheckTx{Code: 1, Log: "invalid public key hex"}, nil
		}
		sigBytes, err := hex.DecodeString(tx.Signature)
		if err != nil {
			return &abcitypes.ResponseCheckTx{Code: 1, Log: "invalid signature hex"}, nil
		}
		if !ed25519.Verify(pubKeyBytes, tx.Payload, sigBytes) {
			return &abcitypes.ResponseCheckTx{Code: 1, Log: "invalid signature — tx rejected"}, nil
		}
	}

	return &abcitypes.ResponseCheckTx{Code: 0}, nil
}

// ── FinalizeBlock ─────────────────────────────────────────────────────────────

func (app *MangoApp) FinalizeBlock(_ context.Context, req *abcitypes.RequestFinalizeBlock) (*abcitypes.ResponseFinalizeBlock, error) {
	app.mu.Lock()
	defer app.mu.Unlock()

	// With separate ABCI instances (one per node), each instance receives
	// FinalizeBlock exactly once per block — no guard needed.
	txResults := make([]*abcitypes.ExecTxResult, len(req.Txs))
	for i, rawTx := range req.Txs {
		var tx Transaction
		if err := json.Unmarshal(rawTx, &tx); err != nil {
			txResults[i] = &abcitypes.ExecTxResult{Code: 1, Log: "decode error"}
			continue
		}
		if err := app.applyTx(&tx); err != nil {
			log.Printf("[Chain] ✗ TX %s (%s): %v", tx.ID[:8], tx.Type, err)
			txResults[i] = &abcitypes.ExecTxResult{Code: 1, Log: err.Error()}
		} else {
			app.state.TxHistory = append(app.state.TxHistory, tx)
			txResults[i] = &abcitypes.ExecTxResult{Code: 0}
		}
	}
	app.state.Height = req.Height

	// ── Autonomous mission management (no external scheduler needed) ──────────
	// These run after every block on every node with the same inputs,
	// producing identical state changes — fully deterministic and decentralised.
	app.autoDispatch(req.Height)
	app.checkCompletions(req.Height)
	app.checkWatchdog(req.Height)

	return &abcitypes.ResponseFinalizeBlock{TxResults: txResults}, nil
}

// missionBlockDuration returns how many blocks a mission should last.
// Blocks are produced every ~1-2 seconds; values match the old time-based durations.
func missionBlockDuration(sev AlertSeverity) int64 {
	switch sev {
	case SeverityCritical: return 60  // ~60s
	case SeverityHigh:     return 90  // ~90s
	case SeverityMedium:   return 120 // ~120s
	default:               return 150 // ~150s
	}
}

// watchdogBlocks is the grace period added on top of mission duration.
// If a drone has not completed by EndBlock+watchdogBlocks it is declared lost.
const watchdogBlocks int64 = 30

var missionOutcomes = []struct{ outcome, details string }{
	{"ROUTE_CLEAR",          "Reconnaissance complete. No obstacles detected on maritime corridor."},
	{"OBSTACLE_DETECTED",    "Unidentified floating object detected. Marked for coast guard."},
	{"VESSEL_ASSISTED",      "Distressed vessel located and escorted to safe waters."},
	{"SUSPICIOUS_ACTIVITY",  "Unusual vessel movement detected. Intel forwarded to consortium."},
	{"FALSE_ALARM",          "Alert investigated. Sensor anomaly confirmed. Area secure."},
}

func outcomeForAlert(alertID string) (string, string) {
	idx := len(alertID) % len(missionOutcomes)
	return missionOutcomes[idx].outcome, missionOutcomes[idx].details
}

// autoDispatch pairs PENDING alerts with IDLE drones and dispatches them.
// Called every block — identical on all nodes — no external scheduler needed.
func (app *MangoApp) autoDispatch(height int64) {
	// Collect PENDING alerts sorted by severity DESC then arrival ASC (FIFO)
	var pending []*AlertState
	for _, a := range app.state.Alerts {
		if a.Status == "PENDING" {
			pending = append(pending, a)
		}
	}
	if len(pending) == 0 {
		return
	}
	// Deterministic sort — same result on every node
	sortAlerts(pending)

	// Collect IDLE drones in deterministic order
	var idle []string
	for id, d := range app.state.Drones {
		if d.Status == "IDLE" {
			idle = append(idle, id)
		}
	}
	if len(idle) == 0 {
		return
	}
	// Sort drone IDs for determinism
	sortStrings(idle)

	dispatched := 0
	for _, alert := range pending {
		if dispatched >= len(idle) {
			break
		}
		droneID := idle[dispatched]
		drone := app.state.Drones[droneID]

		// Find a nation that can pay (round-robin)
		cost := DispatchCost(alert.Severity)
		n := len(nationOrder)
		paidBy := ""
		var acc *NationAccount
		for i := 0; i < n; i++ {
			candidate := nationOrder[(app.state.PaymentIndex+i)%n]
			a, exists := app.state.Accounts[candidate]
			if !exists {
				continue
			}
			if a.Balance >= cost {
				paidBy = candidate
				acc = a
				app.state.PaymentIndex = (app.state.PaymentIndex + i + 1) % n
				break
			}
		}
		if paidBy == "" {
			break
		}

		// Debit payment
		acc.Balance -= cost

		// Update drone state
		drone.Status = "BUSY"
		drone.CurrentAlertID = alert.AlertID
		drone.LastSeen = time.Now()

		// Update alert state with block-based timing
		dur := missionBlockDuration(alert.Severity)
		alert.Status = "ASSIGNED"
		alert.AssignedDrone = droneID
		alert.DispatchCost = cost
		alert.PaidBy = paidBy
		alert.StartBlock = height
		alert.EndBlock = height + dur
		alert.WatchdogBlock = height + dur + watchdogBlocks
		alert.UpdatedAt = time.Now()

		log.Printf("[Chain] 🚁 AUTO-DISPATCH %s → alert %s | cost=%.2f MANGO charged to %s (bal=%.4f) | end_block=%d",
			droneID, alert.AlertID, cost, paidBy, acc.Balance, alert.EndBlock)

		dispatched++
	}
}

// checkCompletions marks missions as complete when their EndBlock is reached.
func (app *MangoApp) checkCompletions(height int64) {
	for _, alert := range app.state.Alerts {
		if alert.Status != "ASSIGNED" {
			continue
		}
		if height < alert.EndBlock {
			continue
		}
		if height >= alert.WatchdogBlock {
			continue // watchdog will handle this
		}

		drone, ok := app.state.Drones[alert.AssignedDrone]
		if !ok {
			continue
		}

		outcome, details := outcomeForAlert(alert.AlertID)

		report := MissionReportPayload{
			DroneID:      alert.AssignedDrone,
			AlertID:      alert.AlertID,
			StartTime:    time.Now().Add(-time.Duration(height-alert.StartBlock) * time.Second),
			EndTime:      time.Now(),
			Outcome:      outcome,
			Details:      details,
			SectorID:     alert.SectorID,
			SensorID:     alert.SensorID,
			AlertType:    alert.Type,
			Location:     alert.Location,
			Severity:     alert.Severity,
			DispatchCost: alert.DispatchCost,
			PaidBy:       alert.PaidBy,
		}
		app.state.Reports = append(app.state.Reports, report)

		drone.Status = "IDLE"
		drone.CurrentAlertID = ""
		drone.LastSeen = time.Now()
		alert.Status = "COMPLETED"
		alert.UpdatedAt = time.Now()

		log.Printf("[Chain] ✅ AUTO-COMPLETE %s finished alert %s → %s (block %d)",
			alert.AssignedDrone, alert.AlertID, outcome, height)
	}
}

// checkWatchdog detects drones that have not completed by WatchdogBlock.
// Marks them CRASHED and re-queues the alert as PENDING.
func (app *MangoApp) checkWatchdog(height int64) {
	for _, alert := range app.state.Alerts {
		if alert.Status != "ASSIGNED" {
			continue
		}
		if height < alert.WatchdogBlock {
			continue
		}

		drone, ok := app.state.Drones[alert.AssignedDrone]
		if !ok {
			continue
		}

		log.Printf("[Chain] ⚠ WATCHDOG: drone %s overdue on alert %s at block %d — marking CRASHED and re-queuing",
			alert.AssignedDrone, alert.AlertID, height)

		now := time.Now()
		drone.Status = "CRASHED"
		drone.CurrentAlertID = ""
		drone.CrashedAt = &now
		drone.LastSeen = now

		// Re-queue the alert
		alert.Status = "PENDING"
		alert.AssignedDrone = ""
		alert.StartBlock = 0
		alert.EndBlock = 0
		alert.WatchdogBlock = 0
		alert.UpdatedAt = now
	}
}

func (app *MangoApp) applyTx(tx *Transaction) error {
	switch tx.Type {
	case TxDeposit:
		return app.applyDeposit(tx)
	case TxTransfer:
		return app.applyTransfer(tx)
	case TxAlert:
		return app.applyAlert(tx)
	case TxDroneDispatch:
		return app.applyDroneDispatch(tx)
	case TxMissionReport:
		return app.applyMissionReport(tx)
	case TxDroneStatus:
		return app.applyDroneStatus(tx)
	default:
		return fmt.Errorf("unknown tx type: %s", tx.Type)
	}
}

func (app *MangoApp) applyDeposit(tx *Transaction) error {
	var p DepositPayload
	if err := json.Unmarshal(tx.Payload, &p); err != nil {
		return err
	}
	acc, ok := app.state.Accounts[p.Nation]
	if !ok {
		return fmt.Errorf("unknown nation: %s", p.Nation)
	}
	rate := app.exchangeRates[p.Fiat]
	if rate == 0 {
		rate = 1.0
	}
	tokens := p.Amount * rate
	acc.Balance += tokens
	log.Printf("[Chain] 💰 DEPOSIT  %s: %.2f %s → +%.4f MANGO  (bal=%.4f)",
		p.Nation, p.Amount, p.Fiat, tokens, acc.Balance)
	return nil
}

func (app *MangoApp) applyTransfer(tx *Transaction) error {
	var p TransferPayload
	if err := json.Unmarshal(tx.Payload, &p); err != nil {
		return err
	}
	from := app.state.Accounts[p.From]
	to := app.state.Accounts[p.To]
	if from == nil || to == nil {
		return fmt.Errorf("unknown account")
	}
	if from.Balance < p.Amount {
		return fmt.Errorf("insufficient balance: have %.4f, need %.4f", from.Balance, p.Amount)
	}
	from.Balance -= p.Amount
	to.Balance += p.Amount
	log.Printf("[Chain] 💸 TRANSFER %s → %s: %.4f MANGO", p.From, p.To, p.Amount)
	return nil
}

func (app *MangoApp) applyAlert(tx *Transaction) error {
	var p AlertPayload
	if err := json.Unmarshal(tx.Payload, &p); err != nil {
		return err
	}
	p.Status = "PENDING"
	app.state.Alerts[p.AlertID] = &AlertState{
		AlertPayload: p,
		CreatedAt:    tx.Timestamp,
		UpdatedAt:    tx.Timestamp,
	}
	log.Printf("[Chain] ⚡ ALERT    %s | %-20s | sev=%d | %s",
		p.AlertID, p.Type, p.Severity, p.SectorID)
	return nil
}

func (app *MangoApp) applyDroneDispatch(tx *Transaction) error {
	var p DroneDispatchPayload
	if err := json.Unmarshal(tx.Payload, &p); err != nil {
		return err
	}
	drone, ok := app.state.Drones[p.DroneID]
	if !ok {
		return fmt.Errorf("unknown drone: %s", p.DroneID)
	}
	if drone.Status != "IDLE" {
		return fmt.Errorf("drone %s not idle (status=%s)", p.DroneID, drone.Status)
	}
	alert, ok := app.state.Alerts[p.AlertID]
	if !ok {
		return fmt.Errorf("unknown alert: %s", p.AlertID)
	}
	if alert.Status != "PENDING" {
		return fmt.Errorf("alert %s already handled (status=%s)", p.AlertID, alert.Status)
	}

	cost := DispatchCost(alert.Severity)
	n := len(nationOrder)
	paidBy := ""
	var acc *NationAccount
	for i := 0; i < n; i++ {
		candidate := nationOrder[(app.state.PaymentIndex+i)%n]
		a, exists := app.state.Accounts[candidate]
		if !exists {
			continue
		}
		if a.Balance >= cost {
			paidBy = candidate
			acc = a
			app.state.PaymentIndex = (app.state.PaymentIndex + i + 1) % n
			break
		}
	}
	if paidBy == "" {
		return fmt.Errorf("no nation has sufficient balance (%.2f MANGO needed)", cost)
	}
	acc.Balance -= cost

	drone.Status = "BUSY"
	drone.CurrentAlertID = p.AlertID
	drone.LastSeen = time.Now()
	alert.Status = "ASSIGNED"
	alert.AssignedDrone = p.DroneID
	alert.UpdatedAt = time.Now()
	alert.DispatchCost = cost
	alert.PaidBy = paidBy

	log.Printf("[Chain] 🚁 DISPATCH %s → alert %s | cost=%.2f MANGO charged to %s (bal=%.4f) | next payer idx=%d",
		p.DroneID, p.AlertID, cost, paidBy, acc.Balance, app.state.PaymentIndex)
	return nil
}

func (app *MangoApp) applyMissionReport(tx *Transaction) error {
	var p MissionReportPayload
	if err := json.Unmarshal(tx.Payload, &p); err != nil {
		return err
	}
	drone, ok := app.state.Drones[p.DroneID]
	if !ok {
		return fmt.Errorf("unknown drone: %s", p.DroneID)
	}
	alert, ok := app.state.Alerts[p.AlertID]
	if !ok {
		return fmt.Errorf("unknown alert: %s", p.AlertID)
	}
	drone.Status = "IDLE"
	drone.CurrentAlertID = ""
	drone.LastSeen = time.Now()
	alert.Status = "COMPLETED"
	alert.UpdatedAt = time.Now()

	// Enrich report with alert data for full audit trail
	p.SectorID = alert.SectorID
	p.SensorID = alert.SensorID
	p.AlertType = alert.Type
	p.Location = alert.Location
	p.Severity = alert.Severity
	p.DispatchCost = alert.DispatchCost
	p.PaidBy = alert.PaidBy
	p.EndTime = time.Now()

	app.state.Reports = append(app.state.Reports, p)
	log.Printf("[Chain] ✅ REPORT   %s | drone=%s alert=%s type=%s loc=%s outcome=%s cost=%.2f paid_by=%s",
		p.EndTime.Format("15:04:05"), p.DroneID, p.AlertID, p.AlertType, p.Location, p.Outcome, p.DispatchCost, p.PaidBy)
	return nil
}

func (app *MangoApp) applyDroneStatus(tx *Transaction) error {
	var p DroneStatusPayload
	if err := json.Unmarshal(tx.Payload, &p); err != nil {
		return err
	}
	drone, ok := app.state.Drones[p.DroneID]
	if !ok {
		return fmt.Errorf("unknown drone: %s", p.DroneID)
	}
	prev := drone.Status
	drone.Status = p.Status
	drone.LastSeen = time.Now()
	if (p.Status == "CRASHED" || p.Status == "OFFLINE") && drone.CurrentAlertID != "" {
		if alert, ok := app.state.Alerts[drone.CurrentAlertID]; ok && alert.Status == "ASSIGNED" {
			alert.Status = "PENDING"
			alert.AssignedDrone = ""
			alert.UpdatedAt = time.Now()
			log.Printf("[Chain] 🔄 RE-QUEUE alert %s (drone %s went %s)", alert.AlertID, p.DroneID, p.Status)
		}
		drone.CurrentAlertID = ""
	}
	if p.Status == "CRASHED" || p.Status == "OFFLINE" {
		now := time.Now()
		drone.CrashedAt = &now
		log.Printf("[Chain] 💥 CRASH    %s | %s → %s", p.DroneID, prev, p.Status)
	} else if p.Status == "IDLE" && (prev == "CRASHED" || prev == "OFFLINE") {
		drone.CrashedAt = nil
		log.Printf("[Chain] 🔧 RECOVERY %s | %s → IDLE", p.DroneID, prev)
	}
	return nil
}

// ── Commit ────────────────────────────────────────────────────────────────────

func (app *MangoApp) Commit(_ context.Context, _ *abcitypes.RequestCommit) (*abcitypes.ResponseCommit, error) {
	// Save state to disk after every committed block.
	// With separate instances, Commit is called exactly once per block per instance.
	// When a node restarts, its ABCI loads the last saved height and CometBFT
	// replays all missed blocks through FinalizeBlock to catch up.
	app.mu.RLock()
	app.saveState()
	app.mu.RUnlock()
	return &abcitypes.ResponseCommit{RetainHeight: 0}, nil
}

// ── Query ─────────────────────────────────────────────────────────────────────

func (app *MangoApp) Query(_ context.Context, req *abcitypes.RequestQuery) (*abcitypes.ResponseQuery, error) {
	app.mu.RLock()
	defer app.mu.RUnlock()
	var result interface{}
	switch req.Path {
	case "/state":
		result = app.state
	case "/accounts":
		result = app.state.Accounts
	case "/drones":
		result = app.state.Drones
	case "/alerts":
		result = app.state.Alerts
	case "/reports":
		result = app.state.Reports
	case "/txhistory":
		h := app.state.TxHistory
		if len(h) > 50 {
			h = h[len(h)-50:]
		}
		result = h
	default:
		return &abcitypes.ResponseQuery{Code: 1, Log: "unknown path"}, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return &abcitypes.ResponseQuery{Code: 1, Log: err.Error()}, nil
	}
	return &abcitypes.ResponseQuery{Code: 0, Value: data}, nil
}

func (app *MangoApp) GetState() *AppState {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.state
}

// ── Sort helpers (deterministic — same result on every node) ─────────────────

func sortAlerts(alerts []*AlertState) {
	sort.SliceStable(alerts, func(i, j int) bool {
		if alerts[i].Severity != alerts[j].Severity {
			return alerts[i].Severity > alerts[j].Severity
		}
		return alerts[i].CreatedAt.Before(alerts[j].CreatedAt)
	})
}

func sortStrings(ss []string) {
	sort.Strings(ss)
}

// ── ABCI stubs ────────────────────────────────────────────────────────────────

func (app *MangoApp) ListSnapshots(_ context.Context, _ *abcitypes.RequestListSnapshots) (*abcitypes.ResponseListSnapshots, error) {
	return &abcitypes.ResponseListSnapshots{}, nil
}
func (app *MangoApp) OfferSnapshot(_ context.Context, _ *abcitypes.RequestOfferSnapshot) (*abcitypes.ResponseOfferSnapshot, error) {
	return &abcitypes.ResponseOfferSnapshot{Result: abcitypes.ResponseOfferSnapshot_REJECT}, nil
}
func (app *MangoApp) LoadSnapshotChunk(_ context.Context, _ *abcitypes.RequestLoadSnapshotChunk) (*abcitypes.ResponseLoadSnapshotChunk, error) {
	return &abcitypes.ResponseLoadSnapshotChunk{}, nil
}
func (app *MangoApp) ApplySnapshotChunk(_ context.Context, _ *abcitypes.RequestApplySnapshotChunk) (*abcitypes.ResponseApplySnapshotChunk, error) {
	return &abcitypes.ResponseApplySnapshotChunk{Result: abcitypes.ResponseApplySnapshotChunk_ACCEPT}, nil
}
func (app *MangoApp) PrepareProposal(_ context.Context, req *abcitypes.RequestPrepareProposal) (*abcitypes.ResponsePrepareProposal, error) {
	return &abcitypes.ResponsePrepareProposal{Txs: req.Txs}, nil
}
func (app *MangoApp) ProcessProposal(_ context.Context, _ *abcitypes.RequestProcessProposal) (*abcitypes.ResponseProcessProposal, error) {
	return &abcitypes.ResponseProcessProposal{Status: abcitypes.ResponseProcessProposal_ACCEPT}, nil
}
func (app *MangoApp) ExtendVote(_ context.Context, _ *abcitypes.RequestExtendVote) (*abcitypes.ResponseExtendVote, error) {
	return &abcitypes.ResponseExtendVote{}, nil
}
func (app *MangoApp) VerifyVoteExtension(_ context.Context, _ *abcitypes.RequestVerifyVoteExtension) (*abcitypes.ResponseVerifyVoteExtension, error) {
	return &abcitypes.ResponseVerifyVoteExtension{Status: abcitypes.ResponseVerifyVoteExtension_ACCEPT}, nil
}
