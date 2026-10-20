# Metallic-MC private Minecraft pilot

This is an executable, deliberately small Linux staging package: one Velocity proxy with Geyser and Floodgate, a flat adventure hub, and a bounded survival world on a second Paper process. It uses installed Java and ordinary child processes. The hub is an empty starter world; no custom map, minigame, matchmaking, production hosting platform, or protected public edge is implied.

The package is separate from the control dashboard. Dashboard telemetry does not launch it. It does not operate Docker, Wings, systemd, firewall rules, domains, router forwarding, or any remote machine. **Do not point it at the legacy Wings deployment on alexbuz1.**

## What is implemented

- `init` creates a **new directory outside the checkout** with private permissions, independent backend configs, and a cryptographically generated forwarding secret. It refuses an existing directory, including any existing world. It leaves both Minecraft EULAs unaccepted.
- `fetch` obtains only four exact upstream builds, verifies their SHA-256 hashes before installation, checks the pinned PaperMC sizes, bounds each download to 200 MiB, and refuses redirects. The download URLs are constructed from validated official project/version/build/hash fields; there is no arbitrary-URL option or automatic updater. An existing mismatched JAR is rejected, not silently replaced.
- `validate` checks routing, authentication, shared-secret consistency, private binding, Bedrock signaling, whitelist enforcement, disabled RCON/query/management listeners, and unexpected plugin JARs. It never prints secrets. `--artifacts` adds checksums for every installed entrypoint JAR.
- `preflight` checks installed Java 25, effective Linux/cgroup memory and CPU allowance, host load, free disk, runtime size, and all four configured ports, including **both TCP and UDP** for Bedrock.
- `run` starts the two backends in dependency order, then the proxy. It waits for actual Java server-list status replies, the Bedrock signaling TCP listener, and Floodgate's generated key file. It runs in the foreground, watches its children and resource headroom, and sends Minecraft console shutdown commands on Ctrl-C, SIGTERM, an explicit stop request, startup failure, or a sustained resource problem.
- `stop` requests shutdown through a private local file and an operating-system-held supervisor lock. It does not signal a saved PID. A stale state file cannot authorize killing an unrelated process. The foreground supervisor reports whether shutdown was clean.
- `allow` adds an explicitly supplied authenticated UUID/name to both backend whitelists while stopped. It grants no operator permissions.
- `backup` creates a compressed stopped-world snapshot with an internal per-file digest manifest and a separate archive SHA-256. `restore` verifies both layers and writes only to a new directory.

The helper currently supports Linux lifecycle operations with visible cgroup v2 accounting. Config generation and static inspection use Python 3.11+ with its standard library; the package does not install Java or request privileged process-management access. `run` refuses execution as root. Resource admission refuses unobservable or legacy cgroup accounting instead of assuming the entire host is available.

## Verified pins

Official metadata was retrieved on **2026-10-20 UTC**. Artifact downloads were then exercised and matched the lock; no Minecraft JVM was started for this verification.

| Component | Version | Exact build | Metadata channel | Role |
|---|---:|---:|---|---|
| Paper | 26.3 | 206 | STABLE | Hub and survival backend |
| Velocity | 4.3.0 | 44 | STABLE | Java proxy |
| Geyser | 2.12.0 | 1265 | default | Bedrock bridge in Velocity |
| Floodgate | 2.2.5 | 141 | default | Bedrock authentication in Velocity |

The exact hashes and metadata URLs are in [`infra/pilot/artifacts.lock.json`](../infra/pilot/artifacts.lock.json). Geyser/Floodgate's `default` channel is their API label; it is not a PaperMC `STABLE` designation. These builds are pinned for a private integration pilot, not certified as a production-compatible combination.

Paper 26.1+ and the current Velocity baseline require Java 25. Geyser currently translates to Java 26.3; this pilot's Paper versions match that protocol target. Geyser is installed only on the proxy. Floodgate on the proxy is sufficient for basic joining; backend Floodgate APIs and additional skin handling are not included. Sources: [Paper requirements](https://docs.papermc.io/paper/getting-started/), [Velocity requirements](https://docs.papermc.io/velocity/getting-started/), [Geyser setup](https://geysermc.org/wiki/geyser/setup/), and [Floodgate setup](https://geysermc.org/wiki/floodgate/setup/).

The **entrypoint artifacts** are reproducible from the lock. This is not an offline, fully hermetic lock for every transitive runtime dependency. Paper's bootstrap may obtain Minecraft/libraries during its first legitimate start, and online account authentication and bridge functionality require appropriate upstream services. Upstream checksums establish content consistency against those publishers, not an independent code audit.

## Private addresses and authentication

| Service | Listen address | Protocol | Access |
|---|---|---|---|
| Velocity | `127.0.0.1:25565` | TCP | Java client on the same host |
| Geyser | `127.0.0.1:19132` | TCP + UDP | Local NetherNet signaling and gameplay |
| Paper hub | `127.0.0.1:25566` | TCP | Velocity modern forwarding required |
| Paper game | `127.0.0.1:25567` | TCP | Velocity modern forwarding required |

Velocity authenticates Java accounts. Paper's `server.properties` therefore uses `online-mode=false`, while `proxies.velocity.online-mode=true` matches the proxy, `proxies.velocity.enabled=true`, and the secret matches the private `forwarding.secret` file. BungeeCord legacy forwarding is disabled. This is the [official modern forwarding configuration](https://docs.papermc.io/velocity/player-information-forwarding/). Modern forwarding still does not replace network isolation or a firewall when a future deployment expands beyond loopback; see [Velocity security](https://docs.papermc.io/velocity/security/).

Geyser uses **NetherNet only**, builtin signaling, a loopback advertised address, and full Bedrock login validation. External NXS registration, assisted joins, STUN discovery, global account linking, and RCON are disabled in this profile. Bedrock 26.50+ fully supports the current NetherNet-only setup; some older versions need the separate RakNet compatibility path described by [Geyser's setup guide](https://geysermc.org/wiki/geyser/setup/). No RakNet port is opened here.

Builtin signaling uses local HTTP; a local Bedrock client may show its documented first-use trust prompt. Serving remote users requires a separate reviewed private-access/HTTPS design. A Java-only SSH port forward does **not** provide the UDP path needed for Bedrock gameplay. This package does not expose the host or register a signaling service on the user's behalf.

## Resource budget

[`infra/pilot/profile.json`](../infra/pilot/profile.json) provides these conservative starter values:

| Budget | Default | Enforcement |
|---|---:|---|
| Proxy/Geyser JVM heap | 1,024 MiB | JVM `-Xmx` |
| Hub JVM heap | 1,536 MiB | JVM `-Xmx` |
| Game JVM heap | 2,048 MiB | JVM `-Xmx` |
| Additional native-memory allowance | 1,536 MiB | Admission calculation |
| Shared host reserve | 2,048 MiB | Admission + sustained-pressure stop |
| Required available memory before start | **8,192 MiB** | Smaller of Linux available RAM and visible cgroup allowance |
| Effective CPUs | At least 4 | Affinity/cgroup admission check |
| Free disk before start/download | At least 10 GiB | Admission check |
| Remaining free disk before stop | 2 GiB | Runtime monitor |
| Pilot directory budget | 6 GiB | Cooperative monitor |
| Players per backend | 12 | Paper whitelist and player cap |
| View / simulation distance | 6 / 4 chunks | Paper config |
| World size | 512 blocks from origin limit | Server properties |

The proxy's displayed maximum is informational; Velocity does not use that display setting as an admission limit. No 24-player performance guarantee is implied. There are no operators initially, and both backends require a whitelist identity.

Only the heap limit is a hard memory limit here. Native memory, CPU, and disk controls are conservative admission/monitoring rules, **not cgroup reservations, hard filesystem quotas, or tenant isolation**. `ActiveProcessorCount=2` influences each JVM's thread ergonomics; it does not cap CPU time. Runtime checks occur approximately every ten seconds; three consecutive low-memory samples request shutdown. Graceful shutdown also needs time and space to save worlds. The pilot cannot promise to protect an already saturated host from unrelated workloads or abrupt power loss.

## Prepare and run

Run from the repository checkout as the intended unprivileged pilot user. Choose a durable, private directory in that user's home; its **parent must already exist**, and the final directory must not exist. Do not use a shared folder or a Git checkout for generated runtime files.

```bash
python3 tools/pilot.py init --root "$HOME/metallic-mc-pilot"
python3 tools/pilot.py validate --root "$HOME/metallic-mc-pilot"
python3 tools/pilot.py fetch --root "$HOME/metallic-mc-pilot"
python3 tools/pilot.py validate --root "$HOME/metallic-mc-pilot" --artifacts
python3 tools/pilot.py preflight --root "$HOME/metallic-mc-pilot" --java /path/to/java25/bin/java
```

`init`, `fetch`, and `preflight` do not accept an EULA and do not launch Minecraft. Preflight reports whether the EULA is accepted; the separate **run gate** blocks until both backend files contain the owner's explicit acceptance.

Before `run`, the owner must personally review the [Minecraft EULA](https://www.minecraft.net/eula) and, if they agree, edit `hub/eula.txt` and `game/eula.txt` in the private runtime directory to contain `eula=true`. This helper intentionally provides no auto-accept option. Generated templates always contain `eula=false`.

Add a known pilot tester before start, using the UUID used by that player's authenticated identity. Do not substitute an arbitrary or offline-generated UUID for an online Java identity. A Bedrock identity must use Floodgate's actual UUID and its prefixed, normalized name. The command validates syntax; it does not prove ownership of an account or resolve names through external services.

```bash
python3 tools/pilot.py allow --root "$HOME/metallic-mc-pilot" \
  --name 'YOUR_AUTHENTICATED_PLAYER_NAME' --uuid 'YOUR_VERIFIED_PLAYER_UUID'
python3 tools/pilot.py run --root "$HOME/metallic-mc-pilot" --java /path/to/java25/bin/java
```

Connect a Java 26.3 client **on the server host** to `127.0.0.1:25565`; connect a compatible local Bedrock client to `127.0.0.1:19132`. Join the hub, use Velocity's `/server game` to visit the survival backend, and `/server hub` to return. The stock proxy command is part of Velocity; see [built-in commands](https://docs.papermc.io/velocity/reference/commands/). The flat hub is a staging arrival area, not a finished art asset.

In another terminal:

```bash
python3 tools/pilot.py status --root "$HOME/metallic-mc-pilot"
python3 tools/pilot.py probe --root "$HOME/metallic-mc-pilot"
python3 tools/pilot.py stop --root "$HOME/metallic-mc-pilot"
```

`stop` acknowledges a request. **Wait for the foreground supervisor to report its final stop result.** It sends `shutdown` to Velocity and `stop` to each backend, allows 45 seconds by default, then terminates only its own child-process handles if necessary. Forced termination is reported as an unclean stop. It must not be treated as a successful save or backup.

Runtime logs are in each role's `console.log` and upstream `logs/` directories. They are private operational data and may contain player metadata despite reduced address logging. The tool removes inherited JVM option injection variables and Velocity's forwarding-secret environment override, so unrelated shell settings cannot silently alter this pinned profile.

## Stopped backups and restoration

Stop the pilot and wait for the supervisor's **clean** final result before taking a snapshot. `backup` acquires the same lifecycle lock, requires a clean recorded shutdown (or a runtime that was never started), checks for same-user processes with a working directory inside the runtime, checks that the configured ports are available, and holds POSIX record locks on existing Minecraft `session.lock` files for the duration. A live, orphaned, locked, or unclean runtime is rejected. If same-user process visibility is restricted, the tool refuses to claim a consistent snapshot.

```bash
python3 tools/pilot.py backup --root "$HOME/metallic-mc-pilot" \
  --output "$HOME/metallic-mc-stopped-2026-10-20.tar.gz"
python3 tools/pilot.py restore --archive "$HOME/metallic-mc-stopped-2026-10-20.tar.gz" \
  --root "$HOME/metallic-mc-restored-2026-10-20"
python3 tools/pilot.py fetch --root "$HOME/metallic-mc-restored-2026-10-20"
python3 tools/pilot.py validate --root "$HOME/metallic-mc-restored-2026-10-20" --artifacts
python3 tools/pilot.py preflight --root "$HOME/metallic-mc-restored-2026-10-20" --java /path/to/java25/bin/java
```

Keep the matching `.tar.gz.sha256.json` file beside the archive. Snapshot outputs never overwrite an existing archive or checksum; restoration never overwrites a directory or world. Restore checks the whole archive digest before creating the destination, then checks every extracted regular file against the internal manifest. It rejects path traversal, links, devices, duplicates, excessive expansion, missing files, and altered contents. A failed restore removes only the new partial destination it created, leaving the original runtime intact.

The snapshot preserves worlds, player state, configuration, previously recorded EULA decisions, whitelists, and private forwarding/Floodgate key material. It omits entrypoint JARs, bootstrap caches/libraries/versions, console/runtime logs, live supervisor state, and transient `session.lock` files; `fetch` reconstructs the pinned entrypoint binaries after restore. Minecraft recreates its world lock files. World locks and snapshot source files must be regular files without hard links. These restrictions prevent opening and closing another descriptor for a held world-lock inode, which would release its classic POSIX record lock, and prevent including outside files through hard-link aliases. This keeps reproducible software and logs out of the world backup. Gzip is not incremental deduplication, and Minecraft region contents may already be compressed.

These backups contain secrets and player data. Files are created with mode `0600`, but **the archive is not encrypted**. Store it in a private destination; any separate off-host copy, encryption, retention policy, or immutable storage arrangement must be configured and tested explicitly. A hash stored beside an archive detects corruption; it does not stop someone who can rewrite both files. This package makes no grief-proof, disk-failure-proof, or DDoS-protection claim.

Consistency is established for the managed, stopped pilot and its world locks. This is not a snapshot mechanism for arbitrary external databases, active editors, or unrelated services. The restore roundtrip is covered with fixture world bytes and private config. A real Minecraft world recovery exercise still requires a properly stopped, authenticated gameplay session and a subsequent client check.

## Validation evidence and remaining acceptance

The implementation has **28 automated safety/lifecycle tests**. They cover EULA refusal before code execution, unique/private secret generation, existing-world preservation, repository/symlink refusal, rejected authentication/binding mutations, nested unreviewed plugin rejection, YAML ambiguity rejection, hash mismatch/partial-download cleanup, tampered-cache refusal, redirect rejection, occupied TCP/UDP ports, resource gates, injected JVM options, child-only graceful shutdown, broken-stdin cleanup, partial-start cleanup, stopped backup/restore roundtrips, live-process and world-lock refusal, lock retention through archive completion, hard-linked/nonregular source refusal, checksum-write cleanup and retry, archive format boundaries, corruption detection, and hostile archive paths. Lifecycle tests use tiny Python child-process fixtures, not a simulated Minecraft engine. The tests that set `eula=true` do so only inside automatically deleted fixture directories and launch no Minecraft software.

```bash
python3 -m unittest discover -s tools -p 'pilot_test.py' -v
```

The pinned official JAR download path was exercised successfully for all four artifacts. Generated configuration validation and checksum validation passed. **Real Java and Bedrock login, world transfer, world save/restart, and gameplay have not been verified.** No EULA was accepted and no Minecraft service was started during package preparation.

Use these separate acceptance results; never substitute a weaker check for a stronger one:

| Check | What it establishes | What it does not establish |
|---|---|---|
| `validate` | Required configuration and security invariants | That Java loaded the files successfully |
| `validate --artifacts` | Entry JAR contents match their exact pins | Complete supply-chain or compatibility review |
| `preflight` | Current resource allowance and port availability | A durable resource reservation |
| `run` startup gate | Java status responses, signaling listener and Floodgate key creation | Authenticated player login or correct skins/inventories |
| `probe` | Java status protocol and Bedrock signaling TCP reachability | Bedrock UDP gameplay or authentication |
| Real Java + Bedrock clients | Login, interaction, transfers and per-edition behavior | Production load/capacity, internet DDoS protection |
| Restart + restore exercise | Persistence and recovery under the tested scenario | Grief-proof or power-loss-proof guarantees |

Before promoting this beyond a private pilot, verify authenticated Java and Bedrock joins and rejections, whitelist behavior, direct-backend login rejection, both directions of server transfer, inventory/UUID consistency, world saving across a clean restart, and tested restoration from a stopped-world backup. Test actual Bedrock movement and interactions over UDP; a successful signaling TCP connection cannot establish this.

The standalone supervisor intentionally has **no service auto-install, reboot hook, automatic update, world reset, backup deletion, public domain, or port-forward action**. For later supervised recovery, first validate this private pilot on a dedicated account and filesystem, then connect its foreground command to a reviewed service manager with process-group cleanup. An abrupt supervisor crash can leave child JVMs running; a new run checks port collisions, and `stop` refuses to act on stale PIDs. Inspect those processes locally rather than deleting lock/state files and blindly starting duplicates.

## Updating this pilot

Review a new upstream release against the same official metadata APIs, change the source lock and any required templates in version control, and test in a **new runtime directory**. There is no `latest` fetch in the launcher. A fetched version is never silently substituted because an exact pin is unavailable. Preserve existing worlds and validated stopped-world backups before any separate migration.

The configuration reader deliberately accepts a conservative subset of YAML and refuses aliases, merges, duplicate keys and unfamiliar syntax. It understands the simple mappings, namespaced keys, quoted scalars and block lists emitted by this profile and usual upstream writers. If a future upstream writer changes the syntax, review/update the parser and tests; do not bypass the validation gate.

## Primary sources

1. [Paper Java requirements and launch behavior](https://docs.papermc.io/paper/getting-started/).
2. [PaperMC Downloads Service, stable-channel guidance, User-Agent requirement and checksum fields](https://docs.papermc.io/misc/downloads-api/).
3. [Paper 26.3 build 206 metadata](https://fill.papermc.io/v3/projects/paper/versions/26.3/builds/206) and [Velocity 4.3.0 build 44 metadata](https://fill.papermc.io/v3/projects/velocity/versions/4.3.0/builds/44).
4. [Velocity Java requirements](https://docs.papermc.io/velocity/getting-started/), [modern forwarding](https://docs.papermc.io/velocity/player-information-forwarding/), and [backend security](https://docs.papermc.io/velocity/security/).
5. [Velocity default TOML at the selected build's source commit](https://github.com/PaperMC/Velocity/blob/4d93ece85bb38b80d82aaf5b03bb33c1355bd6b4/proxy/src/main/resources/default-velocity.toml). The current configuration version is `2.9`, with a `[ping-passthrough]` table.
6. [Geyser proxy setup, NetherNet transport requirements and supported protocol target](https://geysermc.org/wiki/geyser/setup/).
7. [Geyser 2.12.0 build 1265 metadata](https://download.geysermc.org/v2/projects/geyser/versions/2.12.0/builds/1265) and [Floodgate 2.2.5 build 141 metadata](https://download.geysermc.org/v2/projects/floodgate/versions/2.2.5/builds/141).
8. [Geyser configuration fields at the pinned source commit](https://github.com/GeyserMC/Geyser/blob/02dbfaccb8be68c47c176c96c24bdbddb472b375/core/src/main/java/org/geysermc/geyser/configuration/GeyserConfig.java) and [schema migrations](https://github.com/GeyserMC/Geyser/blob/02dbfaccb8be68c47c176c96c24bdbddb472b375/core/src/main/java/org/geysermc/geyser/configuration/ConfigMigrations.java). Schema `10` uses `java.auth-type`; old `remote.auth-type` examples are migrated upstream.
9. [Floodgate setup](https://geysermc.org/wiki/floodgate/setup/) and [Floodgate default configuration at its pinned source commit](https://github.com/GeyserMC/Floodgate/blob/81b65ccb766bf8024858ebf3eefa6974056aecc5/core/src/main/resources/config.yml).
10. [Minecraft EULA](https://www.minecraft.net/eula).

Upstream binaries remain separate downloaded artifacts under their upstream licenses. This repository does not redistribute them or claim ownership of Paper, Velocity, Geyser, Floodgate, or Minecraft.
