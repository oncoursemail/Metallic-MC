# Metallic Presence: offline player view core

This package builds an immutable, bounded player list using the custom policy
evaluator. It is the first source slice of Metallic Presence, not a replacement
for TAB's full packet integration, nametags, scoreboard, boss bars, animations,
configuration editor, or plugin compatibility.

`Build` binds the viewer's authority epoch, instance incarnation, session
generation, context revision, complete policy version/digest, and monotonically
assigned view revision. For each exact candidate ID it evaluates
`network.player.see` on resource type `player`. Denied candidates never enter the
output. Visible candidates sort by descending priority, then stable ID, and the
visible row limit applies after filtering. There are at most 1,024 candidates and
256 output rows; these are implementation bounds, not a supported Minecraft tab
size or player-capacity claim. Client-specific layouts must apply lower limits.

Names and prefixes are literal UTF-8 text with byte limits. Control and format
characters are rejected. No placeholder program, markup parser, HTTP request,
database query, or user-supplied callback runs during rendering. A future packet
adapter must encode this as literal text components, not parse it as MiniMessage.

`Diff` accepts only forward revisions in the same viewer session. A changed
instance/generation/authority epoch requires clearing and rebuilding that
connection's owned channels. The plan removes obsolete rows before upserting
changes, emits no visual changes for unchanged rows, and binds the exact prior
snapshot digest. `Apply(current, plan, now)` updates only the offline model; stale
plans, replayed plans, times before the target's build time, and plans past a known
policy time boundary are rejected. Request shape and the policy clock range are
validated even for an empty candidate list. Returned rows and change lists are
detached copies.

**Trust boundaries:** the caller authenticates the viewer and tenant, validates
mandatory context facts, produces rank/name facts from the same authority
revision, assigns monotonic view revisions, and supplies a trustworthy monotonic
clock for each connection. The immutable model stores build time; it does not
keep a mutable high-water mark of every later validity check or application.
An exact snapshot match and a future policy expiry do not prove distributed
freshness. A malicious caller can construct false inputs. There is no transport
signature, session-ledger lookup, policy freshness lease, or live revocation
channel in this package. The caller must freeze input maps during `Build`.

The current visibility check uses one viewer context for all candidates; richer
viewer/target relationship facts need a separately authenticated adapter.
Temporary policy boundaries expire the whole view conservatively, including
unrelated policy changes. The eventual writer must process timed refreshes and
policy invalidations, recheck validity before transmission, discard stale queues,
serialize ownership of shared scoreboard/team channels, and resynchronize after
an interrupted write. Minecraft packets are not an atomic multi-packet UI commit.

Run `go test ./internal/network/presentation`. Benchmarks here measure a tiny
in-process Go fixture only; they do not compare TAB or establish client latency.
