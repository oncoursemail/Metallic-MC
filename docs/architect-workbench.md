# Private Architect workbench

The workbench is a development surface for the current single administrator. It is disabled by default. It is not the login page for public launcher users.

## Start

Keep the existing controller bound to loopback, configure the existing administrator password and telemetry token, and set:

```dotenv
METALLIC_ARCHITECT_ENABLED=true
```

Run `go run ./cmd/metallic-control`, sign in, and open `/architect`. The infrastructure sidebar shows an Architect link only when the feature is enabled. Host policy and catalog inspection work without Discord credentials. The Discord card reports setup required until its application is configured.

The host registry and allocations are in memory. Restarting the controller invalidates tickets, device credentials and plans. No tunnel, Java process or public listener is created. The existing `metallic-agent` remains a separate Linux telemetry agent and cannot redeem host permissions using its shared token.

## Discord configuration

Create an application that Metallic controls in the Discord developer portal. Register the exact callback URI ending in:

```text
/api/v1/architect/discord/callback
```

Configure these server-side environment variables through a secret manager or protected service environment:

| Variable | Meaning |
| --- | --- |
| METALLIC_DISCORD_CLIENT_ID | Discord application ID |
| METALLIC_DISCORD_CLIENT_SECRET | Server-only application secret; never place it in launcher code |
| METALLIC_DISCORD_REDIRECT_URL | Exact registered callback URI |
| METALLIC_IDENTITY_STORE_PATH | Absolute private path to identity metadata JSON |
| METALLIC_ARCHITECT_ORIGIN | Exact browser origin, such as https://control.example.com |
| METALLIC_COOKIE_SECURE | true when the browser uses HTTPS |

For an isolated local test, the redirect may use a literal loopback IP with HTTP, such as `http://127.0.0.1:8080/api/v1/architect/discord/callback`. The provider configuration deliberately rejects arbitrary cleartext hosts and the ambiguous hostname `localhost`. Production linking requires HTTPS.

The controller still binds to loopback. A trusted HTTPS proxy must route the callback and workbench to the same controller and avoid logging callback query strings or credential-bearing bodies. Do not expose the administration service directly to the internet. If a second authentication proxy intercepts the callback, configure that route deliberately so Discord can return to it; the callback performs its own state, browser nonce and stored-session checks.

Sign in within the previous 15 minutes before linking, unlinking or changing host credentials. Linking requests only identify. No bot, email, messages or guild membership scopes are requested. The temporary Discord grant is revoked after resolving identity, and only identity metadata is saved. Linking is not a Discord role sync or Minecraft authentication flow.

The file store supports one process only. Do not share it across replicas. Back up the metadata under the same access controls as account data. Sessions remain in memory and restart requires signing in again.

## HTTP surface

All routes have prefix `/api/v1/architect/`. JSON request bodies are bounded to 16 KiB, unknown fields and trailing values are rejected, and responses are not cached.

Browser writes require the session cookie and `X-Metallic-Intent: architect`; supplied Origin must match the configured/current origin and cross-site fetches are rejected. Credential changes additionally require recent authentication.

| Method and suffix | Authorization | Result |
| --- | --- | --- |
| GET capabilities | Admin session | Honest feature availability |
| GET discord | Admin session | Current administrator's link metadata |
| POST discord/start | Recent admin session + intent | Authorization URL and expiry; separate HttpOnly flow cookie |
| GET discord/callback | Bound OAuth flow | Validated link result |
| DELETE discord | Recent admin session + intent | Remove local identity association |
| GET hosts | Admin session | Host inventory without credentials |
| POST hosts/pairing | Recent admin session + intent | Single-use ticket, shown once |
| POST hosts/redeem | Pairing ticket | Per-host credential, shown once |
| DELETE hosts/{hostID} | Recent admin session + intent | Revoke host policy access |
| GET servers | Admin session | Allocation plans, no execution claims |
| POST servers | Recent admin session + intent | Bounded server allocation plan |
| POST routes/plan | Individual host bearer credential | Scope/target/expiry policy decision only |
| GET catalog/search | Admin session | Read-only fixed-provider search |
| POST catalog/resolve | Admin session + intent | Exact-release plan and blockers |

The machine redemption request includes `ticket` and `host_name`. It never accepts an owner ID. This private service supplies its configured administrator internally. A public service must resolve account ownership from a durable ticket record rather than copying this single-account adapter.

Server allocation uses `host_id`, `server_id`, `game_port` and `resources` containing integer `cpu_cores`, `memory_mb`, `disk_mb`, `player_slots`. The UI labels memory/disk units as MiB/GiB and converts disk GiB to 1024-MiB units. A plan cannot prove actual machine capacity. See the host policy contract for configured bounds.

A route-plan request uses `host_id`, `server_id`, `target`, and `ttl_seconds`. Its target must be an approved literal loopback game address matching the allocation. The response does not open any socket. There is no public IP, port forwarding, shell or arbitrary command field.

Catalog search parameters are `provider` (hangar/modrinth), `query`, `minecraft_version`, and `platform`. The current UI chooses paper. Resolve accepts `provider`, `project_id`, `version_id`, and `target: {minecraft_version, platform}`. Hangar uses an exact version name; Modrinth uses an exact version ID. Required dependencies are not automatically downloaded.

## Verification and limits

Run the repository CI commands:

```bash
go vet ./...
go test -race -count=1 ./...
go build ./cmd/...
```

Identity tests exercise replay, browser/session binding, expiry, concurrent exchange, account uniqueness and store failures. Controller tests exercise private routing, the feature gate, CSRF defenses and the full host credential lifecycle. Provider tests use deterministic HTTP fixtures; they do not prove a real OAuth application is registered or upstream metadata cannot change.

Browser acceptance covers actual admin login, host pairing/redemption, allocation, route policy and revocation, plus narrow-screen layout. The [validation note](architect-validation.md) records the exact checks completed. Live Discord linking and actual Minecraft sign-in require the owner-controlled applications and remain release gates.

For the system design, upstream evidence, origin/privacy risks, economics, launcher transaction requirements and rollout gates, see [Architect platform design](architect-platform.md). For existing telemetry authentication and infrastructure, see [Security](security.md) and [Architecture](architecture.md).
