# Deployment manifests — pending hardware and network survey

No real network edge, ingress endpoint, port forward, firewall, server orchestration, database, or DDoS mitigation service has been provisioned by this repository.

Deployment design will be pinned to your CPU/RAM/NVMe/Linux/ISP setup, and it will include Java 25, Velocity modern forwarding, Geyser's current NetherNet TCP+UDP requirements, a private backend network, operator-controlled secret distribution, encrypted persistent storage, snapshots, metrics and recovery exercises.

**Avoid copying generic Docker Compose files that expose ports 25565/19132 and the control plane before protective routing is in place.**
