#!/bin/bash
# init-nodes.sh — Initializes 4 CometBFT validator nodes
set -e

COMET_IMAGE="cometbft/cometbft:v0.38.12"
NODES_DIR="./nodes"

echo "=== MangoChain Node Initializer ==="

docker pull $COMET_IMAGE

# Create abci state folders (one per node)
for i in 0 1 2 3; do
  mkdir -p "$NODES_DIR/abci-state-$i"
done
echo "✓ ABCI state folders created"

for i in 0 1 2 3; do
  DIR="$NODES_DIR/node$i"
  mkdir -p "$DIR"
  docker run --rm -v "$(pwd)/$DIR:/cometbft" $COMET_IMAGE init --home /cometbft
  echo "✓ Node $i initialized"
done

declare -a NODE_IDS
for i in 0 1 2 3; do
  DIR="$NODES_DIR/node$i"
  NODE_ID=$(docker run --rm -v "$(pwd)/$DIR:/cometbft" $COMET_IMAGE show-node-id --home /cometbft)
  NODE_IDS[$i]=$NODE_ID
  echo "Node $i ID: $NODE_ID"
done

PEERS=""
for i in 0 1 2 3; do
  if [ -n "$PEERS" ]; then PEERS="$PEERS,"; fi
  PEERS="${PEERS}${NODE_IDS[$i]}@node$i:26656"
done
echo "Peers: $PEERS"

for i in 0 1 2 3; do
  DIR="$NODES_DIR/node$i"
  CONFIG="$DIR/config/config.toml"

  # Each node connects to its own ABCI instance
  sed -i "s|proxy_app = \"tcp://127.0.0.1:26658\"|proxy_app = \"tcp://abci-app-${i}:26658\"|g" "$CONFIG"
  sed -i "s|moniker = \".*\"|moniker = \"node$i\"|g" "$CONFIG"
  sed -i "s|persistent_peers = \"\"|persistent_peers = \"$PEERS\"|g" "$CONFIG"
  sed -i "s|laddr = \"tcp://127.0.0.1:26657\"|laddr = \"tcp://0.0.0.0:26657\"|g" "$CONFIG"
  sed -i "s|cors_allowed_origins = \[\]|cors_allowed_origins = [\"*\"]|g" "$CONFIG"
  sed -i "s|timeout_commit = \".*\"|timeout_commit = \"1s\"|g" "$CONFIG"
  sed -i "s|timeout_propose = \".*\"|timeout_propose = \"2s\"|g" "$CONFIG"

  echo "✓ Node $i configured (proxy_app=abci-app-${i}:26658)"
done

python3 - <<PYEOF
import json, os

nodes_dir = "$NODES_DIR"
genesis_path = f"{nodes_dir}/node0/config/genesis.json"

with open(genesis_path) as f:
    genesis = json.load(f)

genesis["chain_id"] = "mango-chain-1"
validators = []
for i in range(4):
    gpath = f"{nodes_dir}/node{i}/config/genesis.json"
    with open(gpath) as f:
        g = json.load(f)
    if g.get("validators"):
        v = g["validators"][0]
        v["power"] = "10"
        validators.append(v)

genesis["validators"] = validators

for i in range(4):
    dest = f"{nodes_dir}/node{i}/config/genesis.json"
    with open(dest, "w") as f:
        json.dump(genesis, f, indent=2)

print(f"Genesis updated with {len(validators)} validators")
PYEOF

echo ""
echo "=== Initialization Complete ==="
echo "Architecture: single abci-app with persistent state"
echo "Node recovery: abci-app loads state from disk, CometBFT replays missed blocks"
echo ""
echo "Run: docker compose up -d"
