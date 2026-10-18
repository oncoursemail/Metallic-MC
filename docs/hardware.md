# Local server fleet discovery

Before sizing for thousands of players, inventory your own physical equipment. **No hardware specifications have yet been provided, and no remote machine has been configured.**

Complete one row per node. Do not commit public IP addresses, secrets, serial numbers or administration credentials to this repository.

| Node | CPU model / turbo speed | Physical cores | RAM / ECC | NVMe SSD / IOPS | NIC / switch | OS | Purpose |
|---|---|---:|---|---|---|---|---|
| node-01 | TBD | TBD | TBD | TBD | TBD | TBD | Pilot |
| node-02 | TBD | TBD | TBD | TBD | TBD | TBD | Expansion |

Network inventory: **ISP upload and download speeds, dedicated vs residential circuit, public/static IP vs CGNAT, latency to target regions, packet loss/jitter, router throughput, uplink redundancy, UPS runtime, electrical/thermal budget.**

On Linux, an operator can safely collect high-level hardware data with:

```bash
lscpu
free -h
lsblk -o NAME,SIZE,TYPE,ROTA,MODEL
ip -brief link
uname -a
```

Sanitize output before sharing: remove hostnames, MACs, serials, local subnets, public IPs and secrets if privacy is important. Use `fio` or network throughput tools only in agreed staging windows; large tests can exhaust disks or saturate a shared uplink.

Don't purchase hardware yet. CPU high-single-thread performance, thermal consistency, NVMe latency, ECC support and stable low-jitter uplinks often matter more than chasing a very high total core count for every game mode. Folia and multi-instance fleets can take better advantage of additional cores than a monolithic tick loop.
