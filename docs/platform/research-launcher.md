# Metallic launcher and compatibility research

Research date: 2026-10-20. This memo distinguishes upstream behavior from proposed Metallic behavior. No Minecraft client, translator, OAuth application, or managed server was deployed or benchmarked during this research.

## 1. Launcher ownership and authentication

**Recommendation:** Metallic should have its own desktop launcher identity, profile model, UI, update signing, and local server manager. Keep the desktop broker that reads local files and launches Java separate from remote discovery content. A web page rendered inside the launcher must never gain arbitrary shell/file access. The ordinary Minecraft client must remain a supported way to join eligible servers.

Use a Microsoft public-client sign-in flow with the system browser and PKCE. The official Microsoft documentation describes authorization-code + PKCE for desktop applications and system-browser interactive authentication. A shipped desktop executable cannot protect an application-wide client secret. Bind redirects to the sign-in transaction, validate state, and store refresh tokens in the OS credential store. Do not log game session tokens in launch commands or upload them with crash reports. These are Metallic design requirements, not claims about implementation already present.

- Microsoft OAuth flow: https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow.
- Microsoft desktop interactive authentication: https://learn.microsoft.com/en-us/entra/identity-platform/scenario-desktop-acquire-token-interactive.
- Prism's own current AuthFlow source sequences Microsoft sign-in, Xbox user authentication, Xbox authorization for Minecraft Services, launcher login, entitlements, Minecraft profile, then skin retrieval. This provides implementation evidence for the distinction between signing into Microsoft and establishing a playable Minecraft identity: https://github.com/PrismLauncher/PrismLauncher/blob/develop/launcher/minecraft/auth/AuthFlow.cpp.
- Prism supports configuring one's own Microsoft client ID: https://prismlauncher.org/wiki/help-pages/apis/.

Register a Metallic-owned application and validate end-to-end access with an entitled test account before claiming launcher sign-in works. Do not copy another launcher's application ID. Search found recent reports of Minecraft Services application approval/allowlisting failures, but the publicly accessible Microsoft Q&A responses conflict about the exact approval route. I did **not** verify a current authoritative self-service registration process. Treat approved Minecraft Services access as an integration gate, not an assumed consequence of a normal Entra app registration. Do not send the user down ID@Xbox based only on community Q&A.

Java and Bedrock are separate client products. The Java launcher can manage Java profiles on supported desktop systems; Bedrock should open the legitimately installed platform application where the operating system supports doing so. Do not promise a portable Bedrock executable for macOS/Linux or automatic console launching. Official edition overview: https://www.minecraft.net/en-us/article/java-or-bedrock-edition.

## 2. Version-aware installation, not hard-coded Java rules

Resolve each selected Java edition version into an immutable installation plan: exact game metadata, main class, game/JVM arguments, asset index, libraries/native libraries for OS+architecture, Java runtime requirement, checksums, sizes, and URLs. Use upstream hashes for upstream integrity checks and a signed Metallic SHA-256 manifest for the reviewed resolved bundle. Downloads need bounded size, redirect/host policy, safe archive extraction, atomic installation, and rollback. Minecraft tokens belong only in the final process invocation, not reusable profile JSON or synced server files.

The official Mojang version manifest endpoints could not be retrieved through available web tooling and the local sandbox denied network access; **no current manifest bytes or digest were verified here**. Do not invent a pinned version artifact or checksum in the implementation.

Current official release facts materially change the design:

- Minecraft 26.1 requires Java 25 and changed the bundled distribution to Microsoft OpenJDK 25. It also changed default client memory/GC. Source: https://www.minecraft.net/en-us/article/minecraft-java-edition-26-1 (retrieved primary page, find `Java 25`).
- Current Paper guidance maps 26.1+ to Java 25, and 1.20–1.21.11 to Java 21. This is Paper's supported runtime advice, not a universal statement about every older vanilla or modded client. https://docs.papermc.io/paper/getting-started/.
- Prism documents automatic runtime download/selection, but its manual Java table still says 1.20.5+ → Java 21. The automation is a useful product reference; that table must not override Mojang's newer release requirement. https://prismlauncher.org/wiki/getting-started/installing-java/.

Pin client runtime and server runtime independently. A user's chosen old client is not a reason to downgrade the hosted server's Java runtime. Mods/loaders add their own constraints; expose loader/runtime/game-version conflicts before install.

## 3. Exact translation semantics

| Client/server relationship | Required upstream components | Metallic treatment |
|---|---|---|
| Client newer than server | ViaVersion | Publish an exact tested client list per server profile |
| Client older than server, down to 1.9 | ViaVersion + ViaBackwards | Test gameplay features, not only login |
| Client 1.7.x or 1.8.x on newer server | ViaVersion + ViaBackwards + ViaRewind | Separate legacy profile with feature limits |
| Bedrock client → Java server | Geyser, plus ViaVersion when the backend does not accept Geyser's emulated Java version | Separate Bedrock matrix and pack artifacts |
| Arbitrary modded client/server pair | No general guarantee from these plugins | Loader+modpack-specific profile or reject unsupported pairing |

Sources: official ViaVersion README https://github.com/ViaVersion/ViaVersion; ViaBackwards README https://github.com/ViaVersion/ViaBackwards; ViaRewind README https://github.com/ViaVersion/ViaRewind. ViaRewind depends on both other components. The upstream table does not establish that every feature or new release is instantly supported.

Install the translator at **one deliberate protocol boundary**. Upstream installation guidance says either proxy or backend, and explains that proxy placement can impair some legacy visual behavior and anti-cheat integration. ViaVersion's README states Paper/client-side integrations have better state access. Source: https://github.com/ViaVersion/ViaVersion/wiki/Installation.

For managed Paper servers, a protected backend compatibility profile is a sensible default. For untrusted arbitrary plugin servers, security-sensitive enforcement must remain outside the game JVM: a plugin can attempt to disable or mutate other plugin hooks despite a read-only JAR. The immutable layer prevents deletion from normal file management, but it does not make same-process code an isolation boundary. A dedicated translation gateway is an option, with explicitly tested anti-cheat and protocol-state consequences.

ViaBackwards' documented limitations include inaccessible blocks below y=0 or above y=255 for pre-1.17 clients, some inventory-click desynchronization on newer servers, incomplete sound mappings, and smithing-table limitations for some old clients. Therefore publish native/recommended, translated/tested, and legacy/limited capability tiers. Do not advertise old clients as having every new gameplay feature. Source: ViaBackwards README, `Known issues`.

ViaVersion's new-platform guide describes Java/Netty integration and compression-handler ordering. A Go gateway cannot load the Java plugin as if it were a Go library. Retain a tested Java translator boundary while developing Metallic's own gateway, or build an explicitly separate bridge. Review upstream license requirements before distributing/integrating common internals. https://github.com/ViaVersion/ViaVersion/wiki/Creating-a-new-ViaVersion-platform.

## 4. Local, synced, and public modes

These are proposed Metallic semantics:

- **Local private:** no Metallic routing or managed runtime; no surprise plugin installation; local launch, backups, files, logs. Metadata stays local until the user chooses sync/share.
- **Synced managed private:** upload an immutable, coherent snapshot into a staged managed instance; apply the currently accepted platform policy; create new hosted secrets and runtime files. The original local folder remains a distinct copy.
- **Public managed:** same isolation and compatibility admission, plus discovery review, content policy, authenticated gateway sessions, abuse limits, and verified population accounting.
- **Local public through a tunnel:** distinct future tier. A tunnel exposes a local process; it does not convert that process into an isolated Metallic-hosted workload. Do not label it managed or count backend-reported players as verified.

Sync is a release operation, not two live writers editing the same world. Stop/quiesce the source, take a coherent checkpoint, build a complete content-addressed inventory, stage and scan it, compare the remote generation, then promote atomically. If the cloud and local copies both changed, show a fork/conflict choice; never merge region files or silently overwrite the newer copy.

**Critical 26.1 storage change:** Mojang moved dimensions under `dimensions/<namespace>/...`, player data under `players/...`, global data into namespaced directories, and the world resource pack into `resourcepacks/`. A sync implementation that copies only historical `region/`, `DIM-1/`, and `playerdata/` will lose current data. Preserve whole declared world roots with a recognized layout profile, and reject unsupported layouts instead of guessing. Source: official 26.1 release, `World Storage`.

## 5. Resource packs and cross-platform promise

Build original Metallic packs from one source asset model, then emit separately tested Java and Bedrock artifacts. Geyser explicitly does not automatically convert Java packs; native per-backend Bedrock pack changes are constrained to initial connection and require reconnect/transfer handling. https://geysermc.org/wiki/geyser/packs/.

As observed 2026-10-20, Geyser's page states Bedrock 26.30–26.52 and emulated Java 26.3. Older Bedrock versions are unsupported. Treat this as a dated observation, not a permanent support range; planned NetherNet changes remain a separate readiness gate. https://geysermc.org/wiki/geyser/supported-versions/.

Recommended platform improvement: a signed pack compatibility report shows the exact versions/platforms tested, missing/fallback visuals, download size, and whether reconnect is required. Serve reviewed packs from immutable Metallic-controlled URLs. A one-to-one visual claim across Java legacy clients, current Java, and Bedrock would be misleading.

## Candidate implementation now

A pure bounded sync planner is useful before filesystem upload/execution exists. It can validate portable relative inventory paths, reject links and path collisions, enforce size/count limits, require an exact remote generation, exclude secrets and runtime artifacts, and produce a canonical digest. Trusted policy acceptance and source checkpoint attestations must be separate function inputs supplied by the authenticated controller, never claimed by the user's JSON. Its output must remain `executable:false` until a real isolated executor, upload verifier, compatibility admission, and atomic promotion workflow exist.
