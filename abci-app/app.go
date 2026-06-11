package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
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

func (app *MangoApp) CheckTx(_ context.Context, req *abcitypes.RequestCheckTx) (*abcitypes.ResponseCheckTx, error) {
	var tx Transaction
	if err := json.Unmarshal(req.Tx, &tx); err != nil {
		return &abcitypes.ResponseCheckTx{Code: 1, Log: "invalid json"}, nil
	}
	if tx.ID == "" || tx.Type == "" {
		return &abcitypes.ResponseCheckTx{Code: 1, Log: "missing id or type"}, nil
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
	return &abcitypes.ResponseFinalizeBlock{TxResults: txResults}, nil
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
	app.state.Reports = append(app.state.Reports, p)
	log.Printf("[Chain] ✅ REPORT   %s finished alert %s → %s", p.DroneID, p.AlertID, p.Outcome)
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
