# Remote Linux hardware snapshot — 2026-10-18

**Method:** authorized Desktop Commander, read-only shell commands (`lscpu`, `free`, `lsblk`, `df`, `uptime`, `/proc/pressure`, process command names, service activity). **No configuration, reboot, process stop, install or port-forward changes were performed.** Private IP addresses, usernames beyond the device aliases and secrets intentionally omitted. One-time snapshot: values will become stale.

| Feature | `starai` | `alexbuz1` |
|---|---|---|
| OS | Ubuntu 24.04.5 LTS | Ubuntu 24.04.1 LTS |
| CPU | AMD EPYC 7451 (24 cores / 48 threads) | Intel Core i7-9700 (8 cores / 8 threads) |
| RAM | ~125 GiB total, ~44 GiB available at snapshot | ~15 GiB total, ~4.3 GiB available at snapshot |
| Swap | ~7.1 / 8 GiB allocated | ~3.4 / 4 GiB allocated |
| OS filesystem | ~218 GiB ext4, **23 GiB free** (~89% used) | ~231 GiB ext4, **165 GiB free** (~25% used) |
| Additional storage | ~9.8 TB RAID/PERC volume mounted under existing Star AI data paths | ~238.5 GB Samsung NVMe, system disk |
| CPU load average (1 min) | ~20.6 (across 48 hardware threads) | ~7.1 (across 8 cores) |
| Containers | No `docker`/`podman` binary in PATH | Docker daemon **active**; **Wings active** |
| Java | `java` not in PATH | `java` not in PATH |
| GPU | `nvidia-smi` not found | `nvidia-smi` not found |
| Runtime processes | Multiple `llama-server` processes responsible for heavy current compute | `llama-server`, additional active applications, Wings |
| Kernel/cgroups | Linux 6.8; cgroups v2 CPU, cpuset, memory, io, pids | Linux 6.8; cgroups v2 CPU, cpuset, memory, io, pids |

**What this does NOT establish:** real outbound public ISP bandwidth, internet routability, thermal headroom, UPS, DNS or DDoS protection; secure permissions for Wings, installed Java runtimes in Docker images, whether existing workloads can be moved, filesystem IOPS, actual p99 tick latency, usable GPU hardware, or customer-hosting readiness.

## Practical implications

- **Do not install Java game workloads on the nearly full OS partition of `starai` without storage planning.** The larger separate volume currently supports existing Star AI paths; allocating a new world directory there needs explicit ownership and capacity review.
- High swap occupancy is a warning to investigate, **not by itself proof that active pages are currently thrashing**. Track memory pressure and actual swap-in/out rates before changing swap configuration.
- `alexbuz1` already runs a game-server `wings` process. It must not be overwritten by a competing agent. We did not read its credentials or management config.
- Reserve predictable compute for live game sessions; schedule AI training/evaluation in explicit spare-capacity windows and keep Star AI's conversational inference responsive within a capped allocation.
- The EPYC has many CPU threads but is an older architecture; a high tick load may prefer a faster per-core CPU. Verify with reproducible Paper/Folia/Minestom benchmarks, not a theory alone.

## No-touch deployment boundary

This inventory is **read-only**. A production rollout needs operator approval for modifying existing Docker/Wings workloads, available RAM targets, game data path, network bindings/firewall and downtime/rollback windows. The present software cannot enforce capacity or launch a game automatically.
