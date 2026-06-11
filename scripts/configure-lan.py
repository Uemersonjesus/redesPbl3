#!/usr/bin/env python3
"""
configure-lan.py — Configura os nós para deployment em LAN.

Uso:
    # Cenário 1 PC (padrão — reverte para Docker):
    python3 scripts/configure-lan.py

    # Cenário 2 PCs (node0+node1 no PC1, node2+node3 no PC2):
    python3 scripts/configure-lan.py --pc1 192.168.1.10 --pc2 192.168.1.20

    # Cenário 4 PCs (1 nó por PC):
    python3 scripts/configure-lan.py --pc1 192.168.1.10 --pc2 192.168.1.11 --pc3 192.168.1.12 --pc4 192.168.1.13
"""

import sys
import os
import re

# Node IDs fixos (derivados das chaves pré-geradas em nodes/)
NODE_IDS = {
    0: "34355440d13ed5a38d929915a973b4b91f2fb863",
    1: "9bc780d56f58853c7d7e1ded18cab000b2cd7ce4",
    2: "a2c5c5cae377ce4300810f2f69692696ebc2185f",
    3: "ea8208caca0acc9006600e8b992b4d1d4ef63e62",
}

def configure(hosts, ports):
    """
    hosts: dict {node_idx: host}  ex: {0: "192.168.1.10", 1: "node1", ...}
    ports: dict {node_idx: port}  ex: {0: 26656, 1: 26666, ...}
    """
    peers = ",".join(
        f"{NODE_IDS[i]}@{hosts[i]}:{ports[i]}"
        for i in range(4)
    )
    print(f"Peers: {peers}")

    for i in range(4):
        path = f"nodes/node{i}/config/config.toml"
        if not os.path.exists(path):
            print(f"ERRO: {path} não encontrado. Certifique-se que está na pasta raiz do projecto.")
            sys.exit(1)

        with open(path, "r") as f:
            content = f.read()

        # Substituir persistent_peers
        content = re.sub(
            r'persistent_peers = ".*?"',
            f'persistent_peers = "{peers}"',
            content
        )

        with open(path, "w") as f:
            f.write(content)

        print(f"✓ nodes/node{i}/config/config.toml actualizado")

    # Actualizar .env
    env_path = ".env"
    if os.path.exists(env_path):
        with open(env_path, "r") as f:
            env = f.read()

        env = re.sub(r'NODE0_HOST=.*', f'NODE0_HOST={hosts[0]}', env)
        env = re.sub(r'NODE1_HOST=.*', f'NODE1_HOST={hosts[1]}', env)
        env = re.sub(r'NODE2_HOST=.*', f'NODE2_HOST={hosts[2]}', env)
        env = re.sub(r'NODE3_HOST=.*', f'NODE3_HOST={hosts[3]}', env)
        env = re.sub(r'NODE0_RPC_PORT=.*', f'NODE0_RPC_PORT={ports[0]//1}', env)
        env = re.sub(r'NODE1_RPC_PORT=.*', f'NODE1_RPC_PORT={ports[1]//1+1}', env)
        env = re.sub(r'NODE2_RPC_PORT=.*', f'NODE2_RPC_PORT={ports[2]//1}', env)
        env = re.sub(r'NODE3_RPC_PORT=.*', f'NODE3_RPC_PORT={ports[3]//1+1}', env)

        # ABCI_API_URL aponta sempre para o host do node0
        abci_host = hosts[0] if hosts[0] not in ("node0",) else "abci-app-0"
        env = re.sub(r'ABCI_API_URL=.*', f'ABCI_API_URL=http://{abci_host}:8080', env)

        with open(env_path, "w") as f:
            f.write(env)

        print(f"✓ .env actualizado")

    print("\nConfiguração concluída.")


def main():
    args = sys.argv[1:]

    # Parse argumentos simples
    def get_arg(name):
        for i, a in enumerate(args):
            if a == name and i + 1 < len(args):
                return args[i + 1]
        return None

    pc1 = get_arg("--pc1")
    pc2 = get_arg("--pc2")
    pc3 = get_arg("--pc3")
    pc4 = get_arg("--pc4")

    if not any([pc1, pc2, pc3, pc4]):
        # Modo padrão — um único PC com nomes Docker
        print("Modo: 1 PC (padrão Docker)")
        hosts = {0: "node0", 1: "node1", 2: "node2", 3: "node3"}
        ports = {0: 26656, 1: 26656, 2: 26656, 3: 26656}

    elif pc1 and pc2 and not pc3:
        # Modo 2 PCs: node0+node1 no PC1, node2+node3 no PC2
        print(f"Modo: 2 PCs — PC1={pc1}, PC2={pc2}")
        hosts = {0: pc1, 1: pc1, 2: pc2, 3: pc2}
        # node0 usa porta 26656, node1 usa 26666 (ambos no PC1)
        # node2 usa porta 26656, node3 usa 26666 (ambos no PC2)
        ports = {0: 26656, 1: 26666, 2: 26656, 3: 26666}

    elif pc1 and pc2 and pc3 and pc4:
        # Modo 4 PCs: 1 nó por PC
        print(f"Modo: 4 PCs — PC1={pc1}, PC2={pc2}, PC3={pc3}, PC4={pc4}")
        hosts = {0: pc1, 1: pc2, 2: pc3, 3: pc4}
        ports = {0: 26656, 1: 26656, 2: 26656, 3: 26656}

    else:
        print(__doc__)
        sys.exit(1)

    configure(hosts, ports)


if __name__ == "__main__":
    main()
EOF
echo "Script created"