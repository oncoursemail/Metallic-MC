# Metallic Moderation & Hosting Administrative Controls
Status: IMPLEMENTATION SPECIFICATION. The prototype implements a **local simulated workflow only**.

## Distinct principals and targets
- **Player**: Microsoft/Mojang-authenticated player identity with verified Java UUID and Bedrock/XUID mapping where permitted; staff actions may target game network or single game mode.
- **Community hosted server**: a customer's game-server instance, listing, account, and files. Suspension prevents launches and discovery, but data is preserved, owner notified, access limited, appeal possible.
- **Metallic-owned instance**: first-party system service under deployment management; a game moderator does not get machine-root or service lifecycle authority.
- **Physical node**: host administrator only, managed through audited infrastructure plane, never an arbitrary game command.

## Capability matrix
| Capability | Viewer | Player Moderator | Hosting Moderator | Hosting Admin | Root Infra Admin |
|---|---:|---:|---:|---:|---:|
| Browse incident reports | yes | yes | yes | yes | yes |
| Player warn / mute / scoped ban | no | yes | no* | configurable | configurable |
| Take down public listing | no | no | yes | yes | yes |
| Suspend hosted instance | no | no | request only | yes | yes |
| Undo suspension | no | no | request | yes | yes |
| Run server commands | no | no | no | per-owner scope | explicit scope |
| Reinstall / change image | no | no | no | approved & scoped | approved & scoped |
| Soft archive | no | no | no | approved | approved |
| Permanent delete | no | no | no | two approvals / retention | two approvals / retention |
| Physical node power operations | no | no | no | no | approved maintenance |

* Any combined roles explicitly assigned, never implicitly inherited.

The *prototype* intentionally has fewer roles (`viewer`, `moderator`, `administrator`) for testing. Do not treat its client-side gating as the production permission engine.

## Enforcement lifecycle
Report → triage → evidence attached → independent review where required → proposed action → authorization → typed confirmation + reason → idempotent API request → durable audit record → customer notification → appeal → review → reversal or closure.

Soft archive must never be implemented with an unverified `rm -rf` or destructive Wings/Docker call. Separate list removal from data deletion. For permanent deletion: legal retention evaluation, snapshot retention and owner notice with explicit 2-party sign-off; otherwise unsupported.

## API contract (future)
`POST /api/v1/moderation/cases/{caseId}/actions` with bounded action enum, target UUID/ID, reason_code, evidence_ids, idempotency_key, policy_version, expected_server_version and reason. All checks apply in backend and the node agent. Use no arbitrary shell strings, no client-supplied executable paths, no raw startup command changes in moderation API.

`POST /api/v1/instances/{instanceId}/suspend` with tenant ownership verification and reversible state, an audited scheduler transition and explicit notification.

`POST /api/v1/instances/{instanceId}/archive` changes lifecycle only after safe backup + acknowledgement; physical data retention is a separate workflow. Check for active players and drain. A node agent cannot independently decide destructive policy.

Responses include operation_id, current_state, decision_reason, audit_event_id and optional retry_after. On permission denial always return no side effects. Retried requests with matching idempotency_key are safe. Use restrictive rate limits and bind to signed operator session context.

## Logging and security
- Append-only audit database (actor, role, subject, before/after, reason, timestamp, evidence refs, request ID, cryptographic digest chain). Redact player IPs, private chat and secrets; access under strict role policy.
- Permission checks independent of UI visibility. For high-risk actions require MFA re-authentication, review policies, approvals and complete state reconciliation.
- Separation of duties: Star AI can suggest cases and generate code patches but cannot ban players or suspend/delete user servers without authorized human review.
- Evidence: explicit retention and user appeal, privacy review, account security and minors' safety review before public launch.
- Emergency measures: short-lived quarantine requiring timely human review; no indefinite invisible suspensions.

## Threat and failure testing
Privilege escalation via client-side request tampering, mass action accidental scope, cross-tenant access, duplicate request replay, lost node heartbeat, delayed moderation events, webhook retry, spoofed offline account, compromised plugin, abuse of external server origins, command injection, CSRF and session fixation. Include negative integration tests and backup/restore drills.

## Demo mapping
The Studio prototype has example report fixtures. Operator selects role, proposes action, enters a reason, and simulates a state transition with audit entry. `Archive` is reversible with Undo. No network requests and no real credentials. Actual moderation, claims of DDoS protection, billing and deletion remain unimplemented.
