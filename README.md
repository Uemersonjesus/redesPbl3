# 🥭 MangoChain — Strait of Hormuz Consortium

**TEC502 — Problema 3: Economia e Auditoria de Guerra**  
Byzantine Fault Tolerant distributed ledger para coordenação de frota de drones com registo imutável de auditoria.

---

## Arquitectura

```
┌─────────────────────────────────────────────────────────────────┐
│                      CONSENSUS LAYER                            │
│  node0 ←→ node1 ←→ node2 ←→ node3  (CometBFT/Tendermint BFT)  │
│  4 validadores — tolera f=1 nó bizantino (f < n/3)             │
│  Gossip P2P porta 26656 — nós descobrem-se automaticamente     │
└──────┬─────────┬──────────┬──────────┬──────────────────────────┘
       │ ABCI    │          │          │
  abci-app-0  abci-app-1  abci-app-2  abci-app-3
  (node0)     (node1)     (node2)     (node3)
  state em disco — recupera automaticamente após queda
       │
       └── fonte de leitura de todas as dashboards
┌─────────────────────────────────────────────────────────────────┐
│              CAMADA DE APLICAÇÃO                                │
│  Scheduler · Sensores (x3) · Drones (x3)                       │
│  Submetem TXs a qualquer nó com fallback automático            │
└─────────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────────┐
│                   NATION DASHBOARDS                             │
│  :3001 USA   :3002 Israel   :3003 Iran   :3004 Maritime Corp   │
└─────────────────────────────────────────────────────────────────┘
```

### Nota arquitectural

Todas as dashboards lêem o estado de `abci-app-0` como fonte única de verdade. Se `abci-app-0` cair, as dashboards param de actualizar — mas o consenso BFT continua. Cada `abci-app-N` persiste o seu estado em disco e recupera automaticamente após queda via replay de blocos.

---

## Pré-requisitos

```
docker --version        # >= 24.0
docker compose version  # >= 2.20
```

**Windows:** instalar WSL2 antes do Docker Desktop.
```powershell
wsl --install
```

**Linux:** o utilizador deve pertencer ao grupo docker.
```bash
groups  # verificar se "docker" aparece na lista
```

---

## Modo 1 — Tudo num único PC

### Iniciar
```bash
docker compose up --build -d
```

A pasta `nodes/` já vem pré-configurada. Não é necessário correr nenhum script.

### Aceder às dashboards
- USA:          http://localhost:3001
- Israel:       http://localhost:3002
- Iran:         http://localhost:3003
- Maritime:     http://localhost:3004

### Parar (preserva estado)
```bash
docker compose down
```

### Retomar (continua do ponto onde parou)
```bash
docker compose up -d
```

---

## Reset completo — apagar estado da chain

Não existe um comando único para reset. Os ficheiros devem ser removidos manualmente.

### 1. Parar o sistema
```bash
docker compose down
```

### 2. Apagar estado da ABCI

Apagar estes ficheiros (se existirem):
```
nodes/abci-state-0/state.json
nodes/abci-state-1/state.json
nodes/abci-state-2/state.json
nodes/abci-state-3/state.json
```

### 3. Apagar dados do CometBFT

Apagar estes ficheiros dentro de cada pasta nodes/nodeN/data/:
```
nodes/node0/data/blockstore.db
nodes/node0/data/state.db
nodes/node0/data/cs.wal
nodes/node0/data/evidence.db
nodes/node0/data/tx_index.db
```
Repetir para node1, node2 e node3.

### 4. Recriar o ficheiro obrigatório priv_validator_state.json

Após apagar os dados do CometBFT, este ficheiro deve existir em cada nó com o seguinte conteúdo exacto:

Ficheiros a recriar:
```
nodes/node0/data/priv_validator_state.json
nodes/node1/data/priv_validator_state.json
nodes/node2/data/priv_validator_state.json
nodes/node3/data/priv_validator_state.json
```

Conteúdo de cada ficheiro:
```json
{"height":"0","round":0,"step":0}
```

ATENÇÃO: Se este ficheiro não existir ou tiver height diferente de 0, os nós não conseguem iniciar.

### 5. Iniciar do zero
```bash
docker compose up -d
```

### Ficheiros que NUNCA devem ser apagados
```
nodes/nodeN/config/genesis.json
nodes/nodeN/config/config.toml
nodes/nodeN/config/node_key.json
nodes/nodeN/config/priv_validator_key.json
```

---

## Modo 2 — 2 PCs na LAN

### Distribuição
```
PC1 (192.168.100.64): node0 + node1 + scheduler + sensores + drones + gateway-usa + gateway-israel
PC2 (192.168.100.8):  node2 + node3 + gateway-iran + gateway-maritimecorp
```

### Passo 1 — Configurar persistent_peers nos 4 config.toml

Editar os ficheiros abaixo e substituir a linha persistent_peers:
```
nodes/node0/config/config.toml
nodes/node1/config/config.toml
nodes/node2/config/config.toml
nodes/node3/config/config.toml
```

Linha a colocar em TODOS os ficheiros (adaptar IPs se necessário):
```
persistent_peers = "34355440d13ed5a38d929915a973b4b91f2fb863@192.168.100.64:26656,9bc780d56f58853c7d7e1ded18cab000b2cd7ce4@192.168.100.64:26666,a2c5c5cae377ce4300810f2f69692696ebc2185f@192.168.100.8:26656,ea8208caca0acc9006600e8b992b4d1d4ef63e62@192.168.100.8:26666"
```

### Passo 2 — Configurar .env no PC1

```
NODE0_HOST=192.168.100.64
NODE1_HOST=192.168.100.64
NODE2_HOST=192.168.100.8
NODE3_HOST=192.168.100.8

NODE0_RPC_PORT=26657
NODE1_RPC_PORT=26667
NODE2_RPC_PORT=26657
NODE3_RPC_PORT=26667

ABCI_API_URL=http://abci-app-0:8080
```

### Passo 3 — Configurar .env no PC2

ATENCAO: No PC2 o ABCI_API_URL deve usar o IP real do PC1, nao o nome Docker.

```
NODE0_HOST=192.168.100.64
NODE1_HOST=192.168.100.64
NODE2_HOST=192.168.100.8
NODE3_HOST=192.168.100.8

NODE0_RPC_PORT=26657
NODE1_RPC_PORT=26667
NODE2_RPC_PORT=26657
NODE3_RPC_PORT=26667

ABCI_API_URL=http://192.168.100.64:8080
```

### Passo 4 — Copiar o projecto para o PC2

Copiar a pasta completa (incluindo nodes/ ja configurado) para o PC2.

### Passo 5 — Iniciar

PC1 primeiro:
```bash
docker compose -f docker-compose.pc1.yml up --build -d
```

PC2 depois:
```bash
docker compose -f docker-compose.pc2.yml up --build -d
```

Os nos encontram-se automaticamente via gossip P2P. Os blocos comecam a avancar quando ambos os PCs estao activos.

### Aceder às dashboards
pc1
- USA:          http://localhost:3001
- Israel:       http://localhost:3002
Pc2 
- Iran:         http://localhost:3003
- Maritime:     http://localhost:3004

---

## Voltar para modo 1 PC apos ter usado LAN

### Passo 1 — Editar o .env

```
NODE0_HOST=node0
NODE1_HOST=node1
NODE2_HOST=node2
NODE3_HOST=node3

NODE0_RPC_PORT=26657
NODE1_RPC_PORT=26667
NODE2_RPC_PORT=26677
NODE3_RPC_PORT=26687

ABCI_API_URL=http://abci-app-0:8080
```

### Passo 2 — Editar persistent_peers nos 4 config.toml

Substituir a linha persistent_peers em cada ficheiro:
```
nodes/node0/config/config.toml
nodes/node1/config/config.toml
nodes/node2/config/config.toml
nodes/node3/config/config.toml
```

Linha a colocar em TODOS os ficheiros:
```
persistent_peers = "34355440d13ed5a38d929915a973b4b91f2fb863@node0:26656,9bc780d56f58853c7d7e1ded18cab000b2cd7ce4@node1:26656,a2c5c5cae377ce4300810f2f69692696ebc2185f@node2:26656,ea8208caca0acc9006600e8b992b4d1d4ef63e62@node3:26656"
```

### Passo 3 — Fazer reset completo e iniciar

Apagar os ficheiros de estado conforme a seccao de reset acima, recriar o priv_validator_state.json e iniciar:
```bash
docker compose up --build -d
```

---

## Operacoes uteis

### Testar recuperacao de no validador
```bash
docker compose stop node1
# Aguardar 30+ segundos — blocos continuam a avancar
docker compose start node1
# node1 faz replay automatico dos blocos perdidos
```

### Testar queda de drone
```bash
docker compose stop drone-1
# Dashboard mostra OFFLINE, alerta re-enfileirado automaticamente
docker compose start drone-1
# Drone volta ao estado IDLE
```

### Testar falha de consenso
```bash
# Derrubar 2 nos — consenso impossivel com 2/4 nos
docker compose stop node2 node3
# Chain congela — comportamento correcto do BFT
docker compose start node2 node3
```

### Testar particao de rede
```bash
docker network disconnect mango-chain_mango-net node1
# Os outros 3 nos continuam o consenso
docker network connect mango-chain_mango-net node1
# node1 sincroniza automaticamente
```

### URLs de diagnostico
```
http://localhost:26657/status       # Estado do node0
http://localhost:26657/validators   # Validadores activos
http://localhost:8080/api/state     # Estado completo da chain
http://localhost:8080/api/drones    # Estado dos drones
http://localhost:8080/api/txhistory # Ultimas 50 transaccoes
```

---

## Propriedades BFT demonstradas

| Propriedade | Comportamento |
|-------------|---------------|
| Consenso requer 2/3+ | PC1 sozinho nao avanca blocos |
| Tolera f=1 falha | Com 3/4 nos o sistema funciona normalmente |
| Recuperacao apos queda | No sincroniza automaticamente via replay |
| Imutabilidade | Transaccoes visiveis em todos os nos |
| Falha com f>=2 | Chain congela correctamente |

---

## Tipos de Transaccao

| Tipo | Quem envia | O que faz |
|------|-----------|-----------|
| DEPOSIT | Gateway da nacao | Converte fiat -> tokens MANGO |
| TRANSFER | Gateway da nacao | Transfere MANGO entre nacoes |
| ALERT | Sensor | Regista incidente maritimo na fila |
| DRONE_DISPATCH | Scheduler | Atribui drone ao alerta pendente |
| MISSION_REPORT | Scheduler | Regista resultado da missao |
| DRONE_STATUS | Drone | Reporta heartbeat, crash, recuperacao |

## Taxas de Cambio (fiat -> MANGO)
- USD: 1.0 MANGO por dolar
- EUR: 1.08 MANGO por euro
- ILS: 0.27 MANGO por shekel
- IRR: 0.000024 MANGO por rial

---

## Node IDs

| No | Node ID |
|----|---------|
| node0 | 34355440d13ed5a38d929915a973b4b91f2fb863 |
| node1 | 9bc780d56f58853c7d7e1ded18cab000b2cd7ce4 |
| node2 | a2c5c5cae377ce4300810f2f69692696ebc2185f |
| node3 | ea8208caca0acc9006600e8b992b4d1d4ef63e62 |

---

## Portas utilizadas — Modo 1 PC

| Servico | Porta |
|---------|-------|
| node0 RPC | 26657 |
| node1 RPC | 26667 |
| node2 RPC | 26677 |
| node3 RPC | 26687 |
| node0 P2P | 26656 |
| node1 P2P | 26666 |
| node2 P2P | 26676 |
| node3 P2P | 26686 |
| abci-app-0 API | 8080 |
| gateway-usa | 3001 |
| gateway-israel | 3002 |
| gateway-iran | 3003 |
| gateway-maritimecorp | 3004 |

## Portas utilizadas — Modo 2 PCs

| PC | Servico | Porta |
|----|---------|-------|
| PC1 | node0 P2P | 26656 |
| PC1 | node0 RPC | 26657 |
| PC1 | node1 P2P | 26666 |
| PC1 | node1 RPC | 26667 |
| PC1 | abci-app-0 API | 8080 |
| PC1 | gateway-usa | 3001 |
| PC1 | gateway-israel | 3002 |
| PC2 | node2 P2P | 26656 |
| PC2 | node2 RPC | 26657 |
| PC2 | node3 P2P | 26666 |
| PC2 | node3 RPC | 26667 |
| PC2 | gateway-iran | 3003 |
| PC2 | gateway-maritimecorp | 3004 |

---

## Estrutura do projecto
```
mango-chain/
├── abci-app/               # Aplicacao ABCI (Go)
│   ├── main.go             # Servidor ABCI + HTTP API
│   ├── app.go              # Logica de processamento de transaccoes
│   └── types.go            # Tipos e structs do estado
├── scheduler/              # Servico de despacho (Go)
├── sensor/                 # Gerador de alertas (Go)
├── drone/                  # Servico de drone (Go)
├── frontend/               # Dashboard por nacao (Go + HTML)
├── nodes/                  # Dados dos nos (pre-configurados)
│   ├── node0..3/config/    # Chaves e config de cada validador
│   ├── node0..3/data/      # Estado do CometBFT
│   └── abci-state-0..3/    # Estado persistido de cada ABCI
├── .env                    # Configuracao de IPs e portas
├── docker-compose.yml      # Modo 1 PC
├── docker-compose.pc1.yml  # Modo LAN — PC1
└── docker-compose.pc2.yml  # Modo LAN — PC2
```

## Parametros do sistema
- Sensores geram 1 alerta a cada 45-90 segundos
- Missoes duram 60-150 segundos conforme severidade
- Probabilidade de crash simulado: 0.5% por ciclo de 20 segundos
- Dashboards actualizam a cada 4 segundos
- Scheduler despacha por prioridade (severidade) e FIFO
