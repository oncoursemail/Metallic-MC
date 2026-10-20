# Authenticated population observations

This package counts **authenticated sessions observed by trusted gateways**.
It does not prove that a connected account is a human, detect bots, authenticate
Minecraft, or connect to a real gateway. No public or tenant HTTP ingestion
endpoint exists. Backend ping, query, MOTD, plugin messages, NPCs and console
output are never population inputs.

`New(Config)` fixes the authority epoch, server roster, freshness lease and
capacity bounds. An operator adapter registers an exact gateway/boot instance
and its server scopes with `RegisterSource`. The returned opaque in-process
`Source` cannot be created by JSON. `Publish` accepts a full gateway snapshot
with a strictly increasing sequence and canonical authenticated identities:
`java:` followed by a lowercase 32-digit UUID, or `bedrock:` followed by a
positive decimal uint64 XUID without leading zeroes.

The future gateway adapter must verify authentication and the current session
authority fence before publication. It must select admitted gameplay sessions,
exclude pending logins, lobby/queue connections from a destination server's
count, and omit synthetic load-test actors. It must not manufacture a fresh
sequence around a cached observation. The package cannot infer these facts from
an identity string or authenticate a network transport. The grants are local
capabilities, not serializable bearer credentials or signatures.

Each identity has a retained generation watermark. A transfer to another gateway
or server requires a higher generation; previous observations cannot duplicate
or resurrect it. A full snapshot omission closes that observed generation. A
revoked source loses its observations immediately. Source replacement requires
revocation and a new boot instance, and old capabilities remain invalid. These
watermarks remain bounded tombstones; capacity exhaustion rejects new identities
instead of evicting replay protection. They are not persisted: a production
adapter needs durable authority epochs and fences after restart or restore.

Freshness is assigned by the receiver clock (default 15 seconds; configurable
1–60 seconds). Clock rollback fails closed. `Snapshot` exposes `players:null`
when gateway coverage is partial or unavailable. Only complete fresh coverage
can report a count of the received snapshots, including an observed zero. The
separate `observed_authenticated_sessions` value describes only unexpired
observations when coverage is partial. It is not a mathematical lower bound:
a recently disconnected account can remain in a leased observation.
Even a fresh number is a bounded-age observation, not an instantaneous
physical fact or proof of unique humans. Never label it “verified humans”.

The ledger is safe for concurrent use. Copies share private state, and results
and input slices are detached. Defaults: 64 source identities and 100,000 account
watermarks. Hard bounds: 4,096 registered servers and sources, 1,000,000 account
watermarks. This is a control-plane correctness component, not a load-tested
player-capacity claim. Account linking and bot/ranking moderation are separate.

Superseded entries are removed from the previous source when an identity moves,
so retained observations are bounded by the identity limit rather than the
product of sources and identities. Revocation removes a source from expected
coverage; the trusted adapter must fence its actual gateway traffic as part of
retirement before treating the remaining coverage as complete. Revoking this
in-memory capability alone cannot disconnect real Minecraft players.

Run `go test -race ./internal/platform/population`. Regressions cover scope and
authority boundaries, stale sequences, transfer duplication, expiry, incomplete
coverage, revoked boots, closed generations, clock rollback, bounded tombstones,
atomic failure, detached snapshots and concurrent publication.
