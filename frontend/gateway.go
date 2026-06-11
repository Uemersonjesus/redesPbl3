package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type TxType string

const (
	TxDeposit  TxType = "DEPOSIT"
	TxTransfer TxType = "TRANSFER"
)

type Transaction struct {
	ID        string          `json:"id"`
	Type      TxType          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Sender    string          `json:"sender"`
	Payload   json.RawMessage `json:"payload"`
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

var (
	nationID    = getEnv("NATION_ID", "usa")
	nationFiat  = getEnv("NATION_FIAT", "USD")
	abciAPIURL  = getEnv("ABCI_API_URL", "http://abci-app:8080")
	port        = getEnv("PORT", "3000")
	// cometNodes: try each in order, fall back if one is down
	cometNodes = strings.Split(
		getEnv("COMET_RPC_NODES", "http://node0:26657,http://node1:26657,http://node2:26657,http://node3:26657"),
		",",
	)
	// cometRPCURL keeps the first node for consensus proxy (read-only, any node works)
	cometRPCURL = strings.TrimSpace(strings.Split(
		getEnv("COMET_RPC_NODES", "http://node0:26657,http://node1:26657,http://node2:26657,http://node3:26657"),
		",",
	)[0])
)

var rpcClient = &http.Client{Timeout: 2 * time.Second}

var rates = map[string]float64{
	"USD": 1.0,
	"ILS": 0.27,
	"IRR": 0.000024,
	"EUR": 1.08,
}

func main() {
	log.Printf("[Gateway %s] Starting for %s (%s)...", nationID, nationID, nationFiat)

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("/frontend")))
	mux.HandleFunc("/api/info", handleInfo)
	mux.HandleFunc("/api/deposit", handleDeposit)
	mux.HandleFunc("/api/transfer", handleTransfer)
	mux.HandleFunc("/api/ledger", proxyLedger)
	mux.HandleFunc("/api/consensus", proxyConsensus)

	log.Printf("[Gateway %s] Listening on :%s", nationID, port)
	if err := http.ListenAndServe(":"+port, corsMiddleware(mux)); err != nil {
		log.Fatalf("Gateway server failed: %v", err)
	}
}

// handleInfo returns this nation's identity and exchange rate.
func handleInfo(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, map[string]string{
		"nation": nationID,
		"fiat":   nationFiat,
		"rate":   fmt.Sprintf("%.6f", rates[nationFiat]),
	})
}

// handleDeposit converts fiat → MANGO tokens and submits to the chain.
func handleDeposit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 {
		http.Error(w, "invalid amount", http.StatusBadRequest)
		return
	}

	rate := rates[nationFiat]
	tokens := req.Amount * rate

	payload := DepositPayload{
		Nation: nationID,
		Fiat:   nationFiat,
		Amount: req.Amount,
		Tokens: tokens,
		Rate:   rate,
	}
	if err := submitTx(TxDeposit, payload); err != nil {
		log.Printf("[Gateway %s] Deposit error: %v", nationID, err)
		http.Error(w, "failed to submit: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]interface{}{
		"success": true,
		"amount":  req.Amount,
		"fiat":    nationFiat,
		"tokens":  tokens,
		"rate":    rate,
	})
}

// handleTransfer sends MANGO tokens from this nation to another.
func handleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		To     string  `json:"to"`
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount <= 0 || req.To == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	payload := TransferPayload{From: nationID, To: req.To, Amount: req.Amount}
	if err := submitTx(TxTransfer, payload); err != nil {
		http.Error(w, "failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]interface{}{"success": true})
}

// proxyLedger proxies requests to the ABCI app HTTP API.
// Usage: GET /api/ledger?path=/api/state  (defaults to /api/state)
func proxyLedger(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/api/state"
	}
	resp, err := http.Get(abciAPIURL + path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

// proxyConsensus fetches live consensus data from the first available CometBFT node.
func proxyConsensus(w http.ResponseWriter, r *http.Request) {
	// Find first available node
	activeNode := ""
	for _, n := range cometNodes {
		n = strings.TrimSpace(n)
		resp, err := rpcClient.Get(n + "/health")
		if err == nil {
			resp.Body.Close()
			activeNode = n
			break
		}
	}
	if activeNode == "" {
		http.Error(w, "no CometBFT nodes available", http.StatusBadGateway)
		return
	}

	statusResp, err := rpcClient.Get(activeNode + "/status")
	if err != nil {
		http.Error(w, "status fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer statusResp.Body.Close()
	var status map[string]interface{}
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		http.Error(w, "status decode failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	valResp, err := rpcClient.Get(activeNode + "/validators")
	if err != nil {
		http.Error(w, "validators fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer valResp.Body.Close()
	var validators map[string]interface{}
	if err := json.NewDecoder(valResp.Body).Decode(&validators); err != nil {
		http.Error(w, "validators decode failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      status,
		"validators":  validators,
		"active_node": activeNode,
	})
}

// submitTx tries each CometBFT node in order until one accepts.
func submitTx(txType TxType, payload interface{}) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tx := Transaction{
		ID:        uuid.New().String(),
		Type:      txType,
		Timestamp: time.Now(),
		Sender:    nationID,
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
			lastErr = fmt.Errorf("node %s: %w", nodeURL, err)
			continue
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if errField, ok := result["error"]; ok && errField != nil {
			lastErr = fmt.Errorf("node %s rpc error: %v", nodeURL, errField)
			continue
		}
		return nil
	}
	return fmt.Errorf("all nodes failed — last: %w", lastErr)
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
