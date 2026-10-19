# ADR 0003 — Moderation authorization before execution
Status: implemented domain-policy prototype, **not wired to live servers**, October 18, 2026.

The `internal/moderation` package evaluates proposed actions without effects. It rejects unknown actions, unsupported server types, stale MFA, missing evidence and reasons, out-of-scope tenants, spoofable player identities, low-confidence state transitions, archive without restorable backup, and archive/suspension while players are active.

### Domain boundaries
- Player moderation (warn/mute/ban) and hosted instance administration (suspend/unsuspend/archive/restore) require distinct permissions.
- Game-owned servers and legacy Wings servers cannot be controlled through hosted-customer moderation APIs.
- Tenant scoping is explicit even for platform admins.
- Permanent deletion is always denied in the current policy implementation, regardless of role or apparent approval.
- Every proposal has a case reference and idempotency key to support safe future asynchronous application.

### What it DOES NOT do
No persistence, real authentication, network/API routes, Minecraft/Velocity commands, Wings calls, container work, event/audit transactions, or enforcement. Calling Evaluate can only return a Decision. A production API must re-read the current resource state and authorization in one controlled operation and write an append-only audit before dispatching a lifecycle request to a narrowly scoped node agent. A route-specific RBAC matrix must be independently tested, including CSRF, MFA, retry, tenant spoofing and permission-denied side-effect tests.

### Production sequence
Authenticate trusted actor → authorize scoped case action (server-side) → verify evidence and durable state/version → validate action with this policy → append audit decision → atomically mark requested transition → signed authenticated node agent carries out an allowlisted command after game drain/save readiness → reconcile state and notify owner → preserve appeals and reversal.

### Tests
`go test -race -count=1 ./internal/moderation` and `go vet ./internal/moderation` verify current design guards. No performance/capacity claims.
