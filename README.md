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
│  :3001 USA 🇺🇸   :3002 Israel 🇮🇱                              │
│  :3003 Iran 🇮🇷  :3004 Maritime Corp 🚢                        │
└─────────────────────────────────────────────────────────────────┘
```

### Nota arquitectural importante

Todas as dashboards lêem o estado de `abci-app-0` como fonte única de verdade.
**Se `abci-app-0` cair, as dashboards param de actualizar — mas o consenso BFT continua.**
Cada `abci-app-N` persiste o seu estado em disco e recupera automaticamente após queda via replay de blocos.

---

## Pré-requisitos

```bash
docker --version        # >= 24.0
docker compose version  # >= 2.20
```

No Windows é necessário ter o **WSL2** instalado antes do Docker Desktop:
```powershell
# PowerShell como Administrador
wsl --install
# Reiniciar o PC, depois instalar o Docker Desktop
```

---

## Modo 1 — Tudo num único PC

### Iniciar
```bash
docker compose up --build -d
```

A pasta `nodes/` já vem pré-configurada. **Não é necessário correr nenhum script.**

### Aceder às dashboards
| Nação | URL | Moeda |
|-------|-----|-------|
| 🇺🇸 United States | http://localhost:3001 | USD |
| 🇮🇱 Israel | http://localhost:3002 | ILS |
| 🇮🇷 Iran | http://localhost:3003 | IRR |
| 🚢 Maritime Corp | http://localhost:3004 | EUR |

### Parar
```bash
docker compose down
```

### Reset completo (apaga estado da chain)

**Linux/Mac:**
```bash
docker compose down
rm -f nodes/abci-state-*/state.json
rm -f nodes/node*/data/blockstore.db
rm -f nodes/node*/data/state.db
rm -f nodes/node*/data/cs.wal
rm -f nodes/node*/data/evidence.db
rm -f nodes/node*/data/tx_index.db
docker compose up -d
```

**Windows — apagar manualmente no explorador de ficheiros:**
```
nodes/abci-state-0/state.json
nodes/abci-state-1/state.json
nodes/abci-state-2/state.json
nodes/abci-state-3/state.json
nodes/node0/data/blockstore.db
nodes/node0/data/state.db
nodes/node0/data/cs.wal
nodes/node0/data/evidence.db
nodes/node0/data/tx_index.db
(repetir para node1, node2, node3)
```

**O que NUNCA apagar:**
```
nodes/node*/config/                         ← chaves e configuração dos validadores
nodes/node*/data/priv_validator_state.json  ← estado do último voto (obrigatório)
```

Se apagar acidentalmente o `priv_validator_state.json`, recriar com este conteúdo:
```json
{"height":"0","round":0,"step":0}
```

---

## Modo 2 — 2 PCs na LAN (configuração actual)

A configuração actual já tem os IPs da LAN pré-definidos:
- **PC1:** `192.168.100.64` — node0 + node1
- **PC2:** `192.168.100.8` — node2 + node3

### PC1 — comando
```bash
docker compose -f docker-compose.pc1.yml up --build -d
```

### PC2 — antes de iniciar, editar o ficheiro `.env`

Abrir o ficheiro `.env` e alterar a última linha:
```env
ABCI_API_URL=http://192.168.100.64:8080
```

Depois iniciar:
```bash
docker compose -f docker-compose.pc2.yml up --build -d
```

### Ordem de arranque
1. Iniciar PC1 primeiro
2. Iniciar PC2 — os nós encontram-se automaticamente via gossip P2P
3. Os blocos começam a avançar quando ambos os PCs estão activos (consenso requer 3/4 nós)

### Dashboards em LAN
| Nação | URL |
|-------|-----|
| 🇺🇸 United States | http://192.168.100.64:3001 |
| 🇮🇱 Israel | http://192.168.100.64:3002 |
| 🇮🇷 Iran | http://192.168.100.8:3003 |
| 🚢 Maritime Corp | http://192.168.100.8:3004 |

---

## Operações úteis

### Testar recuperação de nó validador
```bash
# Derrubar node1
docker compose -f docker-compose.pc1.yml stop node1

# Aguardar 30+ segundos — blocos continuam a avançar (consenso com 3/4 nós)
# O abci-app-1 guarda o estado em disco

# Recuperar — faz replay automático dos blocos perdidos
docker compose -f docker-compose.pc1.yml start node1
```

### Testar queda de drone
```bash
# Drone reporta OFFLINE via SIGTERM e para sem reiniciar
docker compose stop drone-1

# Dashboard mostra OFFLINE, alerta re-enfileirado automaticamente

# Recuperar drone (simula equipa de manutenção)
docker compose start drone-1
```

### Testar falha de consenso
```bash
# Derrubar 2 nós — consenso impossível com apenas 2/4 nós
docker compose stop node2 node3
# Chain congela — comportamento correcto do BFT

# Recuperar
docker compose start node2 node3
```

### Verificar conectividade entre nós
```bash
# Ver quantos peers cada nó tem
curl http://localhost:26657/net_info
# Deve mostrar "n_peers": "3" quando todos os nós estão activos
```

### URLs de diagnóstico
```
http://localhost:26657/status      # Estado do node0
http://localhost:26657/validators  # Validadores activos
http://localhost:8080/api/state    # Estado completo da chain
http://localhost:8080/api/drones   # Estado dos drones
http://localhost:8080/api/txhistory # Últimas 50 transacções
```

---

## Propriedades BFT demonstradas

| Propriedade | Comportamento |
|-------------|---------------|
| Consenso requer 2/3+ | PC1 sozinho não avança blocos |
| Tolera f=1 falha | Com 3/4 nós o sistema funciona normalmente |
| Recuperação após queda | Nó sincroniza automaticamente via replay |
| Imutabilidade | Transacções visíveis em todos os PCs |
| Falha com f≥2 | Chain congela correctamente |

---

## Tipos de Transacção (todas imutáveis na chain)

| Tipo | Quem envia | O que faz |
|------|-----------|-----------|
| `DEPOSIT` | Gateway da nação | Converte fiat → tokens MANGO |
| `TRANSFER` | Gateway da nação | Transfere MANGO entre nações |
| `ALERT` | Sensor | Regista incidente marítimo na fila |
| `DRONE_DISPATCH` | Scheduler | Atribui drone à alerta pendente |
| `MISSION_REPORT` | Scheduler | Regista resultado da missão |
| `DRONE_STATUS` | Drone | Reporta heartbeat, crash, recuperação |

## Taxas de Câmbio (fiat → MANGO)
- USD: 1.0 MANGO por dólar
- EUR: 1.08 MANGO por euro
- ILS: 0.27 MANGO por shekel
- IRR: 0.000024 MANGO por rial

---

## Node IDs (referência para reconfiguração LAN)

| Nó | Node ID |
|----|---------|
| node0 | `34355440d13ed5a38d929915a973b4b91f2fb863` |
| node1 | `9bc780d56f58853c7d7e1ded18cab000b2cd7ce4` |
| node2 | `a2c5c5cae377ce4300810f2f69692696ebc2185f` |
| node3 | `ea8208caca0acc9006600e8b992b4d1d4ef63e62` |

Se os IPs da LAN mudarem, editar:
1. `persistent_peers` nos 4 ficheiros `nodes/nodeN/config/config.toml`
2. `NODE*_HOST` e `ABCI_API_URL` no ficheiro `.env`

---

## Estrutura do projecto
```
mango-chain/
├── abci-app/               # Aplicação ABCI (Go)
│   ├── main.go             # Servidor ABCI + HTTP API
│   ├── app.go              # Lógica de processamento de transacções
│   └── types.go            # Tipos e structs do estado
├── scheduler/              # Serviço de despacho (Go)
├── sensor/                 # Gerador de alertas (Go)
├── drone/                  # Serviço de drone (Go)
├── frontend/               # Dashboard por nação (Go + HTML)
├── nodes/                  # Dados dos nós (pré-configurados)
│   ├── node0..3/           # Chaves e config de cada validador
│   └── abci-state-0..3/    # Estado persistido de cada ABCI
├── .env                    # Configuração de IPs e portas
├── docker-compose.yml      # Modo 1 PC
├── docker-compose.pc1.yml  # Modo LAN — PC1
├── docker-compose.pc2.yml  # Modo LAN — PC2
└── scripts/
    ├── init-nodes.sh       # Reinicialização (só se apagar nodes/)
    └── configure-lan.py    # Configuração automática de IPs
```

## Parâmetros do sistema
- Sensores geram 1 alerta a cada 45–90 segundos
- Missões duram 60–150 segundos conforme severidade
- Probabilidade de crash simulado: 0.5% por ciclo de 20 segundos
- Dashboards actualizam a cada 4 segundos
- Scheduler despacha por prioridade (severidade) e FIFO

NODE0_HOST=node0
NODE1_HOST=node1
NODE2_HOST=node2
NODE3_HOST=node3

NODE0_RPC_PORT=26657
NODE1_RPC_PORT=26667
NODE2_RPC_PORT=26677
NODE3_RPC_PORT=26687

{"height":"0","round":0,"step":0} reestart
ABCI_API_URL=http://abci-app-0:8080

e altere o .toml de cada arquivo em nodes -> node0 , node1, node2 e node3  por 

persistent_peers = "34355440d13ed5a38d929915a973b4b91f2fb863@node0:26656,9bc780d56f58853c7d7e1ded18cab000b2cd7ce4@node1:26656,a2c5c5cae377ce4300810f2f69692696ebc2185f@node2:26656,ea8208caca0acc9006600e8b992b4d1d4ef63e62@node3:26656"