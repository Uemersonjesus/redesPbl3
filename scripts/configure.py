#!/usr/bin/env python3
"""
MangoChain — Configurador de rede
Configura o .env e os persistent_peers de todos os config.toml.

Uso:
    python3 scripts/configure.py
"""

import os
import re
import sys

# Node IDs fixos das chaves pre-geradas em nodes/
NODE_IDS = {
    0: "34355440d13ed5a38d929915a973b4b91f2fb863",
    1: "9bc780d56f58853c7d7e1ded18cab000b2cd7ce4",
    2: "a2c5c5cae377ce4300810f2f69692696ebc2185f",
    3: "ea8208caca0acc9006600e8b992b4d1d4ef63e62",
}

def ask(prompt, default=None):
    full = f"{prompt} [{default}]: " if default else f"{prompt}: "
    val = input(full).strip()
    return val if val else default

def build_peers(hosts, p2p_ports):
    return ",".join(
        f"{NODE_IDS[i]}@{hosts[i]}:{p2p_ports[i]}"
        for i in range(4)
    )

def update_env(hosts, rpc_ports, abci_url):
    path = ".env"
    if not os.path.exists(path):
        print(f"ERRO: {path} nao encontrado. Execute na pasta raiz do projecto.")
        sys.exit(1)
    with open(path) as f:
        content = f.read()
    content = re.sub(r"NODE0_HOST=.*",     f"NODE0_HOST={hosts[0]}",         content)
    content = re.sub(r"NODE1_HOST=.*",     f"NODE1_HOST={hosts[1]}",         content)
    content = re.sub(r"NODE2_HOST=.*",     f"NODE2_HOST={hosts[2]}",         content)
    content = re.sub(r"NODE3_HOST=.*",     f"NODE3_HOST={hosts[3]}",         content)
    content = re.sub(r"NODE0_RPC_PORT=.*", f"NODE0_RPC_PORT={rpc_ports[0]}", content)
    content = re.sub(r"NODE1_RPC_PORT=.*", f"NODE1_RPC_PORT={rpc_ports[1]}", content)
    content = re.sub(r"NODE2_RPC_PORT=.*", f"NODE2_RPC_PORT={rpc_ports[2]}", content)
    content = re.sub(r"NODE3_RPC_PORT=.*", f"NODE3_RPC_PORT={rpc_ports[3]}", content)
    content = re.sub(r"ABCI_API_URL=.*",   f"ABCI_API_URL={abci_url}",       content)
    with open(path, "w") as f:
        f.write(content)
    print("  .env actualizado")

def update_toml(peers_line):
    for i in range(4):
        path = f"nodes/node{i}/config/config.toml"
        if not os.path.exists(path):
            print(f"  AVISO: {path} nao encontrado, ignorando")
            continue
        with open(path) as f:
            content = f.read()
        content = re.sub(
            r'persistent_peers\s*=\s*"[^"]*"',
            f'persistent_peers = "{peers_line}"',
            content
        )
        with open(path, "w") as f:
            f.write(content)
        print(f"  nodes/node{i}/config/config.toml actualizado")

# ── Modo 1 PC ─────────────────────────────────────────────────────────────────

def mode_single_pc():
    print("\nModo 1 PC — usando nomes Docker internos.")
    hosts     = {0: "node0",  1: "node1",  2: "node2",  3: "node3"}
    p2p_ports = {0: 26656,    1: 26656,    2: 26656,    3: 26656}
    rpc_ports = {0: 26657,    1: 26667,    2: 26677,    3: 26687}
    abci_url  = "http://abci-app-0:8080"
    peers     = build_peers(hosts, p2p_ports)

    print(f"\npersistent_peers = \"{peers}\"")
    if ask("\nAplicar?", "s").lower() != "s":
        print("Cancelado.")
        return

    update_env(hosts, rpc_ports, abci_url)
    update_toml(peers)
    print("\nConfigurado! Comando para iniciar:")
    print("  docker compose up --build -d")

# ── Modo 2 PCs ────────────────────────────────────────────────────────────────

def mode_two_pcs():
    print("\nModo 2 PCs — node0+node1 no PC1, node2+node3 no PC2.")
    ip1 = ask("IP do PC1 (node0+node1)", "192.168.100.64")
    ip2 = ask("IP do PC2 (node2+node3)", "192.168.100.8")
    pc  = ask("Este e o PC1 ou PC2?", "1")

    hosts     = {0: ip1,   1: ip1,   2: ip2,   3: ip2}
    p2p_ports = {0: 26656, 1: 26666, 2: 26656, 3: 26666}
    rpc_ports = {0: 26657, 1: 26667, 2: 26657, 3: 26667}
    abci_url  = "http://abci-app-0:8080" if pc == "1" else f"http://{ip1}:8080"
    peers     = build_peers(hosts, p2p_ports)

    print(f"\npersistent_peers = \"{peers}\"")
    print(f"ABCI_API_URL     = {abci_url}")
    if ask("\nAplicar?", "s").lower() != "s":
        print("Cancelado.")
        return

    update_env(hosts, rpc_ports, abci_url)
    update_toml(peers)
    cmd = "docker compose -f docker-compose.pc1.yml up --build -d" if pc == "1" \
          else "docker compose -f docker-compose.pc2.yml up --build -d"
    print(f"\nConfigurado! Comando para iniciar neste PC:")
    print(f"  {cmd}")

# ── Modo 4 PCs ────────────────────────────────────────────────────────────────

def mode_four_pcs():
    print("\nModo 4 PCs — 1 no por PC.")
    print("Cada PC tem o seu proprio node, abci-app e gateway.")
    print("IMPORTANTE: iniciar sempre o PC do node0 primeiro.\n")

    ip0 = ask("IP do PC com node0 (USA)",          "192.168.1.10")
    ip1 = ask("IP do PC com node1 (Israel)",        "192.168.1.11")
    ip2 = ask("IP do PC com node2 (Iran)",          "192.168.1.12")
    ip3 = ask("IP do PC com node3 (Maritime Corp)", "192.168.1.13")

    print(f"\nEste PC e o node0 ({ip0}), node1 ({ip1}), node2 ({ip2}) ou node3 ({ip3})?")
    pc = ask("Digite 0, 1, 2 ou 3", "0")

    try:
        pc = int(pc)
        if pc not in [0, 1, 2, 3]:
            raise ValueError
    except ValueError:
        print("Opcao invalida.")
        sys.exit(1)

    hosts     = {0: ip0,   1: ip1,   2: ip2,   3: ip3}
    # Em modo 4 PCs cada PC tem so 1 no — todos usam porta 26656 standard
    p2p_ports = {0: 26656, 1: 26656, 2: 26656, 3: 26656}
    rpc_ports = {0: 26657, 1: 26657, 2: 26657, 3: 26657}
    # Todos os PCs leem o estado do node0 (fonte unica de leitura)
    abci_url  = "http://abci-app-0:8080" if pc == 0 else f"http://{ip0}:8080"
    peers     = build_peers(hosts, p2p_ports)

    print(f"\npersistent_peers = \"{peers}\"")
    print(f"ABCI_API_URL     = {abci_url}")
    if ask("\nAplicar?", "s").lower() != "s":
        print("Cancelado.")
        return

    update_env(hosts, rpc_ports, abci_url)
    update_toml(peers)

    cmds = {
        0: "docker compose -f docker-compose.4pc-node0.yml up --build -d",
        1: "docker compose -f docker-compose.4pc-node1.yml up --build -d",
        2: "docker compose -f docker-compose.4pc-node2.yml up --build -d",
        3: "docker compose -f docker-compose.4pc-node3.yml up --build -d",
    }
    print(f"\nConfigurado! Comando para iniciar neste PC (node{pc}):")
    print(f"  {cmds[pc]}")
    print("\nOrdem de arranque recomendada:")
    print(f"  1. PC node0 ({ip0}) — iniciar primeiro")
    print(f"  2. PC node1 ({ip1})")
    print(f"  3. PC node2 ({ip2})")
    print(f"  4. PC node3 ({ip3})")

# ── Main ──────────────────────────────────────────────────────────────────────

def main():
    print("\n MangoChain — Configurador de Rede")
    print("=" * 40)
    print("1 — Tudo num unico PC")
    print("2 — 2 PCs na LAN (node0+node1 / node2+node3)")
    print("3 — 4 PCs na LAN (1 no por PC)")
    choice = ask("\nOpcao", "1")
    if choice == "1":
        mode_single_pc()
    elif choice == "2":
        mode_two_pcs()
    elif choice == "3":
        mode_four_pcs()
    else:
        print("Opcao invalida.")
        sys.exit(1)

if __name__ == "__main__":
    main()
