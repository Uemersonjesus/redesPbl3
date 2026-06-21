# 🥭 MangoChain — Strait of Hormuz Consortium

**TEC502 — Problema 3: Economia e Auditoria de Guerra**
Byzantine Fault Tolerant distributed ledger para coordenação de frota de drones com registro imutável de auditoria.

---

## Arquitetura

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

Todas as dashboards lêem o estado de `abci-app-0` como fonte única de verdade.
Se `abci-app-0` cair, as dashboards param de atualizar — mas o consenso BFT continua.
Cada `abci-app-N` persiste o seu estado em disco e recupera automaticamente após queda via replay de blocos.

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

**Linux:** o usuário deve pertencer ao grupo docker.
```bash
groups  # verificar se "docker" aparece na lista
```

---

## IMPORTANTE — Configuração obrigatória antes de iniciar

O projeto vem com IPs de LAN pré-configurados. Antes de rodar, é necessário escolher o modo de uso e configurar os ficheiros correspondentes.

---

## Modo 1 — Tudo num único PC

### Passo 1 — Editar o ficheiro `.env`

Abrir o ficheiro `.env` na raiz do projeto e colocar exactamente estes valores para executar em um único pc:

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

### Passo 2 — Editar o `persistent_peers` nos 4 ficheiros `config.toml`

Editar os ficheiros abaixo e substituir a linha `persistent_peers`:
```
nodes/node0/config/config.toml
nodes/node1/config/config.toml
nodes/node2/config/config.toml
nodes/node3/config/config.toml
```

Substituir o persistent_peers que esta la por esse em TODOS os ficheiros:
```
persistent_peers = "34355440d13ed5a38d929915a973b4b91f2fb863@node0:26656,9bc780d56f58853c7d7e1ded18cab000b2cd7ce4@node1:26656,a2c5c5cae377ce4300810f2f69692696ebc2185f@node2:26656,ea8208caca0acc9006600e8b992b4d1d4ef63e62@node3:26656"
```

### Passo 3 — Iniciar

```bash
docker compose up --build -d
```

### Acessar às dashboards

|  Nação/Empresa  | URL | Moeda |
|-----------------|-----|-------|
| USA | http://localhost:3001 | USD |
| Israel | http://localhost:3002 | ILS |
| Iran | http://localhost:3003 | IRR |
| Maritime Corp | http://localhost:3004 | EUR |

### Parar (preserva estado)
```bash
docker compose down
```

### Retomar (continua do ponto onde parou)
```bash
docker compose up -d
```

---

## Modo 2 — 2 PCs na LAN

### Distribuição
```
PC1 (ex: 192.168.100.64): node0 + node1 + scheduler + sensores + drones + gateway-usa + gateway-israel
PC2 (ex: 192.168.100.8):  node2 + node3 + gateway-iran + gateway-maritimecorp
```

### Passo 1 — Descobrir os IPs reais de cada PC

**Windows:**
```
ipconfig
```
**Linux:**
```bash
ip addr show
```

### Passo 2 — Editar o `persistent_peers` nos 4 ficheiros `config.toml`

Editar os ficheiros abaixo em TODOS os PCs (usar os IPs reais da vossa LAN):
```
nodes/node0/config/config.toml
nodes/node1/config/config.toml
nodes/node2/config/config.toml
nodes/node3/config/config.toml
```

Linha a colocar em TODOS os ficheiros (substituir pelos IPs reais):
```
persistent_peers = "34355440d13ed5a38d929915a973b4b91f2fb863@IP_PC1:26656,9bc780d56f58853c7d7e1ded18cab000b2cd7ce4@IP_PC1:26666,a2c5c5cae377ce4300810f2f69692696ebc2185f@IP_PC2:26656,ea8208caca0acc9006600e8b992b4d1d4ef63e62@IP_PC2:26666"
```

Exemplo com os IPs atuais:
```
persistent_peers = "34355440d13ed5a38d929915a973b4b91f2fb863@192.168.100.64:26656,9bc780d56f58853c7d7e1ded18cab000b2cd7ce4@192.168.100.64:26666,a2c5c5cae377ce4300810f2f69692696ebc2185f@192.168.100.8:26656,ea8208caca0acc9006600e8b992b4d1d4ef63e62@192.168.100.8:26666"
```

### Passo 3 — Editar o `.env` no PC1

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

### Passo 4 — Editar o `.env` no PC2

> ATENCÃO: no PC2 o ABCI_API_URL deve usar o IP real do PC1, não o nome Docker.

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

### Passo 5 — Copiar o projeto para o PC2

Copiar a pasta completa (incluindo `nodes/` já configurado) para o PC2 via pen drive ou partilha de rede.

### Passo 6 — Iniciar

**PC1 primeiro:**
```bash
docker compose -f docker-compose.pc1.yml up --build -d
```

**PC2 depois:**
```bash
docker compose -f docker-compose.pc2.yml up --build -d
```

Os nós encontram-se automaticamente via gossip P2P. Os blocos começam a avançar quando ambos os PCs estão ativos — o consenso requer 3/4 nós.

### Dashboards em LAN
É possível acessar apenas as interfaces do node que estão naquele pc
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

Apagar estes ficheiros dentro de cada pasta `nodes/nodeN/data/`:
```
nodes/node0/data/blockstore.db
nodes/node0/data/state.db
nodes/node0/data/cs.wal
nodes/node0/data/evidence.db
nodes/node0/data/tx_index.db
```
Repetir para node1, node2 e node3.

### 4. Recriar o ficheiro obrigatório `priv_validator_state.json`

Após apagar os dados do CometBFT, recriar este ficheiro em cada nó com o conteúdo exacto abaixo:

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

> Se este ficheiro não existir ou tiver height diferente de 0, os nós não conseguem iniciar.

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

## Operações úteis

### Testar recuperação de nó validador
```bash
docker compose stop node1
# Aguardar 30+ segundos — blocos continuam a avançar com 3/4 nós
docker compose start node1
# node1 faz replay automático dos blocos perdidos
```

### Testar queda de drone
```bash
docker compose stop drone-1
# Dashboard mostra OFFLINE, alerta re-enfileirado automaticamente
docker compose start drone-1
```

### Testar falha de consenso
```bash
# Derrubar 2 nós — consenso impossível com 2/4 nós
docker compose stop node2 node3
# Chain congela — comportamento correcto do BFT
docker compose start node2 node3
```

### Testar partição de rede
```bash
docker network disconnect mango-chain_mango-net node1
# Os outros 3 nós continuam o consenso
docker network connect mango-chain_mango-net node1
# node1 sincroniza automaticamente
```

### URLs de diagnóstico
```
http://localhost:26657/status       # Estado do node0
http://localhost:26657/validators   # Validadores activos
http://localhost:8080/api/state     # Estado completo da chain
http://localhost:8080/api/drones    # Estado dos drones
http://localhost:8080/api/txhistory # Ultimas 50 transações
http://localhost:8080/api/reports   # Laudos de missões
```

---

## Propriedades BFT demonstradas

| Propriedade | Comportamento |
|-------------|---------------|
| Consenso requer 2/3+ | PC1 sozinho não avanca blocos |
| Tolera f=1 falha | Com 3/4 nós o sistema funciona normalmente |
| Recuperacão após queda | nó sincroniza automaticamente via replay |
| Imutabilidade | transações visiveis em todos os nos |
| Falha com f>=2 | Chain congela correctamente |

---

## Tipos de Transação

| Tipo | Quem envia | O que faz |
|------|-----------|-----------|
| DEPOSIT | Gateway da nacao | Converte fiat -> tokens MANGO |
| TRANSFER | Gateway da nacao | Transfere MANGO entre nacoes |
| ALERT | Sensor | Regista incidente maritimo na fila |
| DRONE_DISPATCH | Scheduler | Atribui drone ao alerta pendente |
| MISSION_REPORT | Scheduler | Regista resultado da missao |
| DRONE_STATUS | Drone | Reporta heartbeat, crash, recuperação |

## Taxas de Cambio (fiat -> MANGO)
- USD: 1.0 MANGO por dolar
- EUR: 1.08 MANGO por euro
- ILS: 0.27 MANGO por shekel
- IRR: 0.000024 MANGO por rial

---

## Node IDs (referencia para configuracao LAN)

|   Nó  | Node ID |
|-------|---------|
| node0 | 34355440d13ed5a38d929915a973b4b91f2fb863 |
| node1 | 9bc780d56f58853c7d7e1ded18cab000b2cd7ce4 |
| node2 | a2c5c5cae377ce4300810f2f69692696ebc2185f |
| node3 | ea8208caca0acc9006600e8b992b4d1d4ef63e62 |

Se os IPs da LAN mudarem, editar:
1. `persistent_peers` nos 4 ficheiros `nodes/nodeN/config/config.toml`
2. `NODE*_HOST` e `ABCI_API_URL` no ficheiro `.env`

---

## Estrutura do projeto
```
mango-chain/
├── abci-app/               # Aplicacao ABCI (Go)
│   ├── main.go             # Servidor ABCI + HTTP API
│   ├── app.go              # Logica de processamento de transações
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
- Missões duram 60-150 segundos conforme severidade
- Probabilidade de crash simulado: 0.5% por ciclo de 20 segundos
- Dashboards atualizam a cada 4 segundos
- Scheduler despacha por prioridade (severidade) e FIFO
