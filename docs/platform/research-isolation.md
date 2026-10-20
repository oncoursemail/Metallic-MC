# Metallic tenant isolation and server lifecycle research

Research date: 2026-10-20. Primary upstream documentation was inspected live. These are design recommendations, not claims that Metallic currently launches or isolates tenant servers. No remote host or production configuration was changed.

## Recommended trust model

Treat every tenant JAR, mod, native library, world import, console response, resource pack, and server status reply as untrusted input. Syncing changes where a workload executes; it does not make its code safe. The platform must enforce its rules from components the tenant does not control.

Model **execution location** separately from **visibility** and **platform enrollment**:

| Product state | Execution authority | Metallic obligations and controls |
| --- | --- | --- |
| Local private | User's launcher on their machine | No platform route, listing, forced plugin, or cloud moderation command; local files remain theirs. |
| Hosted private | Metallic node executor | Hosted-use policy applies; isolated runtime, quotas, protected compatibility layer, private access list. Private does not mean unrestricted use of Metallic infrastructure. |
| Hosted public | Metallic node executor | All hosted controls plus listing approval, verified edge population, public content rules and abuse response. |
| Local public tunnel | User controls local runtime; Metallic controls route/listing | Public participation rules and authenticated edge access apply. Revocation can disconnect the tunnel and remove the listing. Metallic cannot attest that arbitrary local software follows internal policy or silently administer the user's computer. |

Allow a local private server to stay local forever. “Sync now” should show a specific transfer plan and required hosted profile before upload. “Disconnect from Metallic” revokes future routing and synchronization; it cannot retroactively remove already retained backups or enforcement records, so retention must be visible.

## Mandatory compatibility components

An immutable image or read-only managed mount can prevent tenant filesystem deletion of the canonical component. It cannot make a plugin inside a hostile JVM an independent security boundary. Paper's PluginManager explicitly exposes disabling plugins [S1]; current Java no longer offers the old Security Manager isolation mechanism [S2]. Therefore enforce protocol compatibility, authenticated identity, population measurement, routing and network restrictions outside the tenant JVM. Keep any required backend adapter read-only and validate it at admission, but do not trust its self-report as attestation.

Recommended managed layout: signed/pinned base runtime, a host-owned compatibility service at the edge, a tenant data volume, a bounded temporary volume, and host-owned policy metadata. The owner-facing file manager exposes tenant data only. Deleting a managed component through that API returns a clear managed-component error. If the immutable image or manifest fails verification, the node blocks admission and recreates the managed instance from the approved artifact. It does not download an arbitrary “latest” JAR on every boot. Local private instances are outside this reconciliation loop.

## Isolation choice and deployment gates

Kubernetes documents that ordinary containers share the host kernel and recommends VM or userspace-kernel sandboxing for intentionally malicious workloads [S3]. Rootless Docker reduces daemon/container privilege, but its documentation distinguishes that from user-namespace remapping, where the daemon remains privileged [S4]. These are layers, not an absolute escape-proof claim.

For a small initial controlled pilot, use separate project containers and a deliberately limited runtime catalog. Before accepting arbitrary third-party plugins, qualify one of these profiles with real Java startup, world I/O, networking, native-library, backup and stress tests:

| Candidate | Strength | Qualification work |
| --- | --- | --- |
| gVisor per tenant | Interposes a userspace kernel between application syscalls and host kernel [S5]. | JVM/plugin/native API compatibility, world I/O and p99 tick latency. gVisor notes file and network I/O performance costs [S6]. |
| Firecracker per tenant | Dedicated guest kernel and small VMM; jailer drops privileges and isolates the VMM [S7]. | KVM-capable host, patched host/guest/microcode, network setup, disk lifecycle, safe snapshots, control-plane operations. |

My preferred target for **arbitrary uploaded code** is one microVM per server, with typed host-side operations; gVisor is a viable earlier profile if benchmarks and compatibility pass. This is an engineering choice for Metallic's threat model, not a measured claim that either option is universally faster or more secure. Never silently fall back from a required sandbox to a plain container because the sandbox fails.

Every profile requires non-root workload identity, no privileged mode, no host PID/network/IPC namespace, no host filesystem or runtime socket, dropped capabilities, no privilege escalation, a maintained seccomp/LSM policy, a read-only root, server-specific storage, bounded scratch, and no control-plane credentials in the guest. Docker's default seccomp policy is an allowlist and blocks sensitive operations; disabling it removes a defense [S8]. Hosts and the orchestrator remain privileged trust components.

## Network policy is a separate boundary

gVisor explicitly says that sandboxing does not protect a reachable vulnerable host API, and relies on host mechanisms for network/resource control [S5]. Firecracker does not filter guest packets; its production guide requires host filtering and describes network/storage rate limiting [S9].

Use default-deny ingress and egress per server. Permit game traffic only from Metallic's authenticated edge to the allocated game port. Deny host administration, databases, other tenants, cloud metadata, LAN/private management ranges, IPv6 equivalents, link-local endpoints, and arbitrary Internet relays. Approved plugin integrations receive narrow per-server destinations/capabilities through an egress broker; users cannot provide an arbitrary destination URL and turn Metallic into an SSRF proxy. Check addresses after DNS resolution, during redirects and at connection time. Package downloads belong in a separate bounded artifact fetcher, not an unrestricted tenant JVM network policy.

An explicitly allowed connection still needs application authorization. Never trust a network source address alone to authorize a console, file operation, SQL query or host command. Authenticate controller-to-agent channels, bind credentials to one server and generation, and rotate them after restore/reallocation. Velocity documentation says forwarding protection supplements, rather than replaces, a firewall [S10]. Use protected forwarding plus private backend reachability during the comparison phase.

If Kubernetes is introduced later, namespaces alone are not the full boundary. NetworkPolicy needs an enforcing network plugin. Existing connections may or may not be closed when policy changes [S11]. Therefore suspension must actively revoke edge sessions, close relay flows and stop or isolate the managed workload; merely writing a deny rule is not a completed suspension. Docker deployments also require rules in the correct engine-managed packet path; appending to FORWARD can be too late [S12].

## Resource fairness and diagnostics

Enforce host cgroup v2 CPU, memory, process/thread and I/O budgets, plus disk bytes/inodes, network bytes/packets/connections and log retention. Linux documents independent CPU bandwidth, memory, PID and I/O controllers; a PID limit is needed because a fork storm can exhaust tasks before memory, and memory limits may invoke the cgroup OOM killer [S13]. JVM heap is only part of total memory usage. Reserve headroom for native memory, thread stacks, mappings, network buffers and the sandbox itself.

The control service reports desired state and observed state separately. “Start requested” is not “running.” A TCP port opening is not proof of authenticated Minecraft play. Diagnose crashes by correlating exit code, cgroup OOM counters, JVM error files, bounded recent logs, restart history and profile/version changes. Preserve uncertainty: a watchdog trace can identify a blocked thread without proving which plugin caused it.

## File manager and import boundary

Authorize every request by authenticated principal, tenant, server, action and current server generation. Unpredictable IDs do not replace object authorization; OWASP explicitly requires object-level checks for APIs that operate on user-supplied IDs [S14]. An exported link, console ticket or upload capability must be scoped and short-lived. Re-check authorization at use time and close existing channels after revocation.

Do not implement secure paths using string-prefix checks or `Clean` followed by `Open`. Go's official guidance explains symlink and rename races between path checking and file use. `os.Root` was added in Go 1.24 for traversal-resistant operations; a Go 1.23 executor needs a suitable descriptor-relative implementation or a reviewed safe-open library instead [S15]. A lexical manifest validator can reject suspicious names but cannot claim filesystem confinement.

For import, bound compressed bytes, expanded bytes, file count, path depth, per-file size and processing time. Reject absolute paths, parent traversal, drive/UNC names, backslashes in the portable manifest, control characters, reserved device names, trailing dots/spaces, duplicate canonical paths and file/directory ancestor conflicts. Reject symlinks, hard links, devices, sockets and FIFOs. Treat case collisions and cross-platform normalization explicitly. Extract to a new quarantined directory with a rooted API, never over the active world. Verify bytes against the content-addressed manifest, create new files exclusively, sanitize modes, and atomically promote only after all checks pass. File-upload defense requires multiple independent checks and size limits; antivirus alone does not establish safety [S16].

Exclude credentials, environment files, private keys, session tokens, old platform agents, managed binaries and the launcher account store from automatic synchronization. Show exclusions. World/config/plugin imports are separate categories; copying a plugin is acceptance to run untrusted code under the chosen sandbox, not a “clean scan” guarantee.

## Console boundary

The Minecraft console is a bounded stream to a supervised Minecraft process, not a host shell. Do not accept executable paths, environment variables, mount paths, Docker flags, arbitrary PIDs, script snippets or service names in that API. Use typed start/stop/restart/hibernate operations for lifecycle.

For a browser console, use authenticated secure WebSockets, exact origin checks, per-message authorization, revocation on logout/expiry, payload limits and backpressure; OWASP documents why a successful WebSocket handshake does not grant unlimited action privileges [S17]. Render logs as escaped text with a restricted ANSI parser, bound output queues, redact secrets and avoid storing tokens in query URLs. Console-write and plugin-upload permissions both grant substantial in-server control and should be explicitly delegated.

## Sync, hibernation and single-writer safety

Do not bidirectionally synchronize a live Minecraft world as if it were a collection of independent office documents. Proposed safe MVP: stop/quiesce the local writer, create a manifest-bound snapshot, upload to quarantine, validate, restore into an isolated candidate, validate boot, then atomically assign the hosted server's new generation. Keep the previous snapshot until promotion succeeds. Repeated Sync Now uses a base revision; concurrent edits become an explicit conflict or a new branch, never a blind overwrite.

At most one executor owns the writable world. A database lease or monotonically increasing number does not physically prevent a disconnected old process from writing to its disk. Before takeover, prove the old writer exited and detach/revoke its storage, or use a storage system that actually fences old writers. Kubernetes ReadWriteOnce means one node and can permit multiple pods on that node; it is not a general single-process lock [S18]. Storage-level reservation protocols are an example of actual enforced I/O fencing [S19].

“Hibernate” should initially mean orderly stop plus a verified durable world/config checkpoint and released compute reservation. On wake, start a fresh process, revalidate the current managed profile, and assign a new generation. Do not promise instant in-memory resume. Firecracker snapshots contain memory and machine state; upstream treats snapshot files as trusted, warns connections may not survive, and explains that reusing a snapshot can repeat identifiers/random seeds/tokens [S20]. Never load a tenant-uploaded VMM snapshot. A future managed memory-snapshot path must authenticate/encrypt all components, control one-time use, regenerate identity, and prove runtime-version compatibility.

## Lifecycle semantics

Every accepted operation has a stable operation ID, authenticated actor, expected server revision, authority epoch, runtime generation and audit reason. Execution performs compare-and-swap before work; stale operations cannot act on a replacement instance. A plan is a proposal, not an executable capability.

| Operation | Required sequence and failure semantics |
| --- | --- |
| Start | Policy acceptance, pinned compatibility profile, available isolated capacity; prepare then boot with ingress closed; observe readiness before admitting traffic. |
| Admit | Running observation, current runtime/compatibility attestation, gateway health, public moderation approval where applicable; open only the assigned route. |
| Restart | Close admission, drain, checkpoint, stop old writer, revoke generation, start isolated replacement, re-check readiness. |
| Stop | Close routes, drain, stop, verify exit. A timeout is an explicit forced-stop choice, with possible recovery from last durable checkpoint. |
| Hibernate | Stop safely, persist checkpoint and verify manifest, release compute; do not call it hibernated while a writer remains alive. |
| Suspend hosted | Immediately remove discovery/revoke sessions, fence operations, stop/isolate runtime, quarantine retained data. Owner cannot unsuspend it by restart/sync. |
| Suspend local public | Remove listing/revoke tunnel credentials and sessions; do not delete or stop the user's independently hosted private server. |
| Reinstate | Clear suspension only after review; leave server stopped/unlisted until ordinary admission checks pass. |
| Delete hosted | Tombstone server and revoke routes/credentials; quarantine data with defined retention. Later purge is an independently authorized irreversible workflow. |
| Export | Produce an owner-scoped archive excluding platform secrets/components. Do not start a suspended server merely to export it. Evidence retention/abuse restrictions need explicit policy. |

Plugin updates should use restart and rollback snapshots; Paper warns that plugin reload is known to cause problems [S21]. Crash loops need bounded retry with backoff and a quarantine state, not infinite automatic restarts that consume capacity.

## Implementation boundary for this turn

The existing `internal/hosting` service records owner-scoped host enrollment, allocation and route policy; its documentation explicitly says it does not start processes or relay traffic. Extend this with a **pure typed lifecycle planner**, not a pretend executor. Separate request data from trusted authoritative evidence; default-deny missing acceptance, stale state, absent attestations and unsupported actions. Return exact blockers, typed steps, expected fences and `execution_enabled:false`. Root HTTP adapters must derive owner/role/server evidence from authentication and server records, never deserialize these claims from the requesting tenant. Production execution, durable operations, sandbox provisioning, file confinement and real gameplay remain later gates.

## Primary source register

- **S1** Paper PluginManager API, including disablePlugin/clearPlugins: https://jd.papermc.io/paper/1.21.11/org/bukkit/plugin/PluginManager.html (web ref retrieved primary page).
- **S2** Oracle Java 25 security guide, Security Manager disabled as of JDK 24: https://docs.oracle.com/en/java/javase/25/security/security-manager-is-permanently-disabled.html.
- **S3** Kubernetes multi-tenancy: https://kubernetes.io/docs/concepts/security/multi-tenancy/.
- **S4** Docker rootless mode: https://docs.docker.com/engine/security/rootless/.
- **S5** gVisor security model: https://gvisor.dev/docs/architecture_guide/security/.
- **S6** gVisor production guide: https://gvisor.dev/docs/user_guide/production/. The page itself says its tuning guidance focuses on an existing trusted stack rather than arbitrary user-provided code.
- **S7** Firecracker jailer: https://github.com/firecracker-microvm/firecracker/blob/main/docs/jailer.md.
- **S8** Docker seccomp: https://docs.docker.com/engine/security/seccomp/.
- **S9** Firecracker production host setup: https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md.
- **S10** Velocity security: https://docs.papermc.io/velocity/security/.
- **S11** Kubernetes network policies: https://kubernetes.io/docs/concepts/services-networking/network-policies/.
- **S12** Docker and iptables: https://docs.docker.com/engine/network/firewall-iptables/.
- **S13** Linux cgroup v2: https://docs.kernel.org/admin-guide/cgroup-v2.html.
- **S14** OWASP API object authorization: https://api-security.owasp.org/editions/2023/en/0xa1-broken-object-level-authorization/.
- **S15** Go traversal-resistant file APIs: https://go.dev/blog/osroot.
- **S16** OWASP file upload: https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html.
- **S17** OWASP WebSocket security: https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html.
- **S18** Kubernetes persistent volumes: https://kubernetes.io/docs/concepts/storage/persistent-volumes/.
- **S19** AWS EBS NVMe reservations (example, not a selected Metallic dependency): https://docs.aws.amazon.com/ebs/latest/userguide/nvme-reservations.html.
- **S20** Firecracker snapshots: https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md.
- **S21** Paper commands and plugin reload warning: https://docs.papermc.io/paper/reference/commands/.

All branch-based documentation links are moving upstream references. Pin runtime artifacts and source revisions in the implementation release manifest before executing them.
