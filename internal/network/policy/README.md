# Metallic network policy core

This package compiles an immutable policy document and makes deterministic local authorization decisions. It is an original, deliberately small first component for Metallic's network stack. It contains no HTTP handler, storage, RPC, Minecraft command execution, protocol adapter, or live authentication integration.

The current controller's administrative moderation policy remains separate. This package has not replaced LuckPerms or become a live Minecraft enforcement point.

## API

```go
snapshot, err := policy.Compile(document)
if err != nil {
    // Reject the entire candidate document; retain the previous snapshot.
    return err
}
version := snapshot.Version()
decision := snapshot.Evaluate(policy.Request{
    Expected:     version,
    TenantID:     "metallic",
    SubjectID:    "java:alice",
    Action:       "world.build",
    ResourceType: "world",
    ResourceID:   "main",
    Context: map[string]string{
        "server": "survival",
        "world":  "main",
    },
    Now: trustedClock.Now(),
})
```

The example assumes an authenticated internal caller has selected the subject, resource, complete context, current snapshot and clock. Request is an internal data type, not a request-body schema. A syntactically valid subject identifier is not proof of Java, Bedrock, Discord, or administrator identity.

`ValidateRequest(Request) error` lets adapters validate identifier grammar, every supplied context pair and clock range before iterating targets, including when the target set is empty. Invalid structure returns `ErrInvalidRequest` without echoing input values. `ValidRequestTime(time.Time) bool` accepts nonzero instants in UTC years 1970–9999, regardless of the supplied time zone. Neither helper authenticates facts, establishes required context completeness, matches a policy version, or proves clock monotonicity or security freshness. Evaluate performs the same structural validation before authorization.

Document contains a schema version, publisher epoch and revision, one tenant, roles, registered subjects with role memberships, and rules. A rule selects exactly one subject or role, one action, one resource type, and one resource identifier. Context and time windows can restrict both rules and subject memberships. Role-to-parent edges are unconditional in this first version.

## Exact semantics

1. Compilation validates the whole document, including unused roles, subjects, memberships and rules. A cycle, unknown reference, duplicate ID, identical duplicate membership, invalid context, unsupported effect, contradictory window or exceeded bound rejects the complete document.
2. An active membership gives its subject the named role and that role's transitive parents. All memberships whose time and context conditions match contribute; there is no primary-group preference.
3. Context pairs are an AND of exact, case-sensitive key/value comparisons. Extra valid request context pairs do not change a rule's match. A missing required pair makes that membership or rule inapplicable. In particular, omitting a context can make a contextual denial inapplicable: a future authenticated integration must establish required context completeness before evaluation.
4. Actions, resource types, resource IDs, role IDs and subject IDs match exactly. Separators have no implicit hierarchy. There are no wildcards, regexes, numerical priorities, platform defaults, or automatic operator privileges. A context value containing a star is a literal value.
5. Every matching deny takes precedence over every matching allow. An inherited denial therefore defeats a direct temporary allowance. If there is an allow and no deny, access is allowed. Otherwise, it is denied.
6. Windows are half-open: NotBefore is inclusive and ExpiresAt is exclusive. A zero bound is unbounded. Temporary expiration is evaluated at the supplied Now; it does not depend on a cleanup timer.
7. The lowest lexicographic ID among deciding rules supplies RuleID. For an inherited rule, InheritancePath is the shortest active path from an assigned role to the rule's role; equal-length paths are ordered lexicographically by their complete sequence of role IDs. A direct subject rule has no role path. The selected witness is an audit explanation, not a precedence rule that can override a denial.

For example, if Alice belongs to moderator, moderator inherits member, and member allows world.build on world/main, the witness is [moderator, member]. A matching deny on either Alice or any active inherited role defeats that allowance. An expired moderator membership contributes no role, even if its associated rules are permanent.

## Complete snapshot identity

Version contains Epoch, Revision and a SHA-256 Digest. Compile hashes a domain-separated canonical encoding of the entire document, including its schema, epoch, revision, tenant, all roles/parents, all subjects/memberships and their windows/contexts, and every rule field. The digest is a content identifier, not a signature or an authentication credential.

Compilation sorts set-like collections and context keys, canonicalizes nil/empty collections, and normalizes timestamps to UTC without monotonic-clock metadata. The same logical document with reordered arrays, map insertions or equivalent time-zone representations produces the same version. Changing any canonical document field changes the digest, including a publisher accidentally reusing an epoch and revision for different contents.

Evaluate requires all three Expected fields to match its snapshot. The package does not maintain a latest-revision registry, compare revisions across epochs, authenticate the publisher, publish snapshots, or prevent a caller from deliberately selecting an old snapshot. A future publisher must durably enforce revision uniqueness and monotonic publication; an enforcer must choose the permitted snapshot independently of the player's input.

## ValidUntil describes time semantics

An evaluated, known action gets the earliest future NotBefore or ExpiresAt anywhere in the compiled document as ValidUntil. This deliberately conservative boundary includes unrelated subjects and rules. It can invalidate a cache earlier than necessary, but it includes future denials and memberships that are currently inactive.

At the boundary itself, recompute. Caching only until the winning allowance's expiry would be incorrect when a permanent allowance is restricted by a denial scheduled to start sooner.

A zero ValidUntil means the document has no remaining scheduled boundary. **ValidUntil is not a freshness lease.** It says nothing about later policy edits, identity changes, session revocation, issuer availability or network partitions. A future cache must additionally enforce authenticated session/context versions, publisher freshness limits and local revocation fences. This package intentionally provides no offline-authority guarantee.

## Immutability and execution bounds

Compile owns private copies of all maps and slices used by a Snapshot. Version returns a value copy; decisions receive detached witness slices. Concurrent evaluations do not mutate internal state. Callers must keep input maps stable while Compile or Evaluate is reading them; after compilation they may freely change the original document.

| Bound | Value |
|---|---:|
| Canonical document JSON | 4 MiB |
| Roles | 256 |
| Subjects | 4,096 |
| Rules | 4,096 |
| Parents per role | 16 |
| Memberships per subject | 32 |
| Longest inheritance path, including endpoints | 32 roles |
| Context pairs per membership, rule or request | 16 |
| Identifier or context-key length | 128 bytes |
| Context-value length | 256 bytes |

Identifiers use the ASCII grammar `[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}`. Context values must be nonempty valid UTF-8, have no leading/trailing whitespace, and contain no Unicode control or format characters. Bounds are measured in bytes. Nonzero timestamps must represent a UTC year from 1970 through 9999. Rule and membership windows with both bounds present must have increasing instants.

Compilation validates the DAG and precomputes one shortest/lexicographic path per reachable role. It never enumerates every path through a diamond graph. Lookup examines the subject's bounded memberships, their bounded role closures, and rules indexed by exact action. It has no database or network access and does not run policy scripts, placeholder functions or regular expressions. Worst-case lookup still depends on the documented membership, role, path, rule and context limits; the package makes no constant-time performance claim.

## Decision codes

| Code | Meaning |
|---|---|
| uninitialized | No compiled snapshot was supplied |
| invalid_request | A request identifier, context or clock is invalid |
| version_mismatch | Epoch, revision or document digest differs |
| tenant_mismatch | Request tenant differs from the document |
| unknown_subject | The document does not register the subject |
| unknown_action | No document rule names the action |
| no_matching_allow | No active, matching allowance exists |
| explicit_deny | An active matching denial determined the result |
| explicit_allow | At least one active allowance matched and no denial matched |

Validation errors wrap ErrInvalidDocument and describe the invalid field class without echoing context values. The API supplies one stable deciding witness, not a complete administrative policy-explanation service. It also does not write audit records; an enforcing application must record the decision with its authenticated request and state transition.

## Verification

```sh
go test ./internal/network/policy
go test -race ./internal/network/policy
go vet ./internal/network/policy
go test ./internal/network/policy -run '^$' -fuzz FuzzExactContextHasNoPatternExpansion -fuzztime=5s
go test ./internal/network/policy -run '^$' -bench . -benchmem
```

Tests exercise tenant isolation, every version component, deny precedence, conditional/temporary inheritance, exact time boundaries, future-deny cache boundaries, graph cycles/depth, maximum rule count, malformed unused data, canonical order invariance, detached ownership and concurrent compilation/evaluation. The fuzz target checks that arbitrary context strings either fail validation or remain literal exact matches.

On 2026-10-20, the package's normal tests, race tests and vet passed. A five-second run of the context fuzz target completed 134,798 executions without failure.

The included benchmarks measure a small local fixture with two roles, two subjects and one rule. On the same date, Linux amd64, an AMD EPYC 9V74 executor, GOMAXPROCS=2 and the available Go 1.27.2 toolchain, three 200 ms samples measured inherited evaluation at 426–474 ns/op with 32 B/op and one allocation for the returned witness. Small-document compilation measured about 7.0–7.4 microseconds/op and 4,160 B/op. These are average local microbenchmarks, not tail-latency, maximum-size, game-server, packet-adapter or cross-network results. The code uses Go 1.23 APIs and the repository's Go 1.23 language level.

## Relationship to existing software

LuckPerms already provides [context-aware cached data](https://javadoc.io/static/net.luckperms/api/5.5/net/luckperms/api/cacheddata/CachedDataManager.html), [separate recalculation and invalidating reload behavior](https://javadoc.io/static/net.luckperms/api/5.5/net/luckperms/api/cacheddata/CachedDataManager.Container.html), and [the node and inheritance origin that caused a result](https://javadoc.io/static/net.luckperms/api/5.5/net/luckperms/api/cacheddata/Result.html). This core does not implement its full configurable precedence, wildcard, metadata, integration or administration feature set.

The default-deny/deny-overrides rule is a deliberate semantic choice. [Cedar's authorization documentation](https://docs.cedarpolicy.com/auth/authorization.html) describes those combination rules and also a skip-on-error behavior. This core instead rejects malformed documents before they become usable snapshots. The future distributed design must add authenticated context provenance and explicit revocation/freshness contracts before these decisions can enforce live network access.
