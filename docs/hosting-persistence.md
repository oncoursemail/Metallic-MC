# Architect hosting persistence

Architect can preserve its enrolled hosts and resource allocations across a
controller restart. This is an optional private Linux file store for the existing
single-administrator controller. Architect remains disabled by default.

## Enable the store

Set both the existing Architect enable flag and an absolute store path in the
controller environment:

```dotenv
METALLIC_ARCHITECT_ENABLED=true
METALLIC_HOSTING_STORE_PATH=/var/lib/metallic/hosting/state.json
```

The final directory must belong to the user running the controller and have mode
`0700`. Newly created directories use that mode. Existing shared directories are
rejected rather than having their permissions silently changed. The state and
adjacent `state.json.lock` files must belong to the same user, have mode `0600`,
be regular files, and have no hard links. Store paths cannot pass through
symlinks. Ancestors must belong to the controller user or root and must not be
writable by other users, except for directories with the sticky bit such as
`/tmp`.

Use a persistent local filesystem that supports Linux advisory locks, atomic
rename, file sync and directory sync. Keep the store outside the public web root,
repository and a container's disposable layer. Network shares and multiple
replicas are outside this storage mode's support boundary. On other operating
systems, selecting persistence returns an explicit error; memory-only hosting
and the agent remain available.

Without a store path the hosting policy continues to use memory. Configuring a
path fails startup if it cannot be safely opened or validated. The controller
must not silently fall back to an empty, memory-only store after an error.

## What survives a restart

| Record | Recovery behavior |
| --- | --- |
| Enrolled host ID, owner and display name | Restored exactly |
| Host credential | Only its SHA-256 digest is stored; the original random credential still authenticates until expiry or revocation |
| Credential expiry | Preserved; a restart never extends it |
| Revocation | Preserved, including revoked and expired bounded host records |
| Server allocation and resource reservation | Restored with its owner, host, resource quantities and reserved game port |
| Allocation status | Remains `policy_registered`; it does not assert that a server was provisioned or is running |
| Pairing ticket | Discarded; issue a fresh ticket after restart |
| Route decision | Not stored; a caller must request a new decision |
| Controller login sessions and telemetry | Not stored by this feature |

The hosting registry is independent of node heartbeat telemetry. Controller node
telemetry starts empty after restart and becomes available only after a fresh
authenticated agent heartbeat. A restored enrollment or allocation cannot make a
node appear online. This feature does not create a relay, open a game listener,
deploy a game server or restart an operating-system service.

## Commit and recovery guarantees

Each enrollment, allocation and first revocation follows the same sequence while
holding the service mutex:

1. Build a candidate snapshot without changing published in-memory state.
2. Validate the candidate against the same schema and policy rules used during
   recovery.
3. Write a random private temporary file beside the current state, sync its
   contents and close it.
4. Atomically rename it over the state file and sync the containing directory.
5. Publish the new in-memory snapshot and return success.

A pre-rename failure returns `hosting.ErrStore`. The prior snapshot and resource
accounting remain in effect; a failed enrollment keeps its pairing ticket
retryable while the process and ticket remain active. The HTTP integration maps
storage failures to service-unavailable responses. No failed enrollment returns
a host credential.

A failure syncing the directory after rename has an uncertain durable outcome:
the new file may already be visible. In that case the service discards pending
tickets and refuses every further hosting operation, including route decisions
and listings. It never continues granting access from an older in-memory
snapshot. Close and reopen the controller after fixing the filesystem problem;
recovery syncs the directory and validates the file that actually exists.

After an uncertain commit, reconcile the host and allocation lists before
retrying. An enrollment may exist even though its response failed and its
one-time credential was never returned. Revoke that unusable enrollment and pair
again. A failed revocation response must never be treated as confirmation that a
revocation succeeded.

The controller holds an exclusive advisory writer lock for the store's entire
lifetime. A second live controller receives an error. A process exit releases
the OS lock automatically, without a stale-lock removal procedure. Do not remove
or replace the `.lock` file while a controller is running; it is deliberately
left on disk when the service closes.

## Validation and backup boundaries

The version-1 JSON schema is limited to 32 MiB and to the configured host and
allocation counts. Recovery rejects malformed data, duplicate JSON keys,
case-alias keys, unknown fields, missing revocation flags, duplicate IDs or
credential digests, invalid dates, broken ownership references, unexpected
allocation statuses, disallowed ports and aggregate resource overcommit. Reducing
the configured resource or record limits below existing reservations therefore
requires an explicit migration. Invalid files are left intact for diagnosis.

The state contains no plaintext host credentials, pairing tokens or session
tokens. Credential digests are derived from independently generated 256-bit
credentials. They are not user passwords. Keep the file and its backups private:
host ownership and infrastructure metadata are still sensitive, and the local
controller user or root can change that file.

This feature is restart persistence, not a backup service. Atomic replacement
provides a consistent file to copy, but a backup can omit the latest commit unless
the controller is stopped. Use a real copy or encrypted backup, not a hard link.
Restore only with the controller stopped and preserve the required ownership and
permissions. Restoring an old snapshot also restores its old revocation and
allocation history; reapply subsequent revocations before resuming access. The
store cannot detect rollback to an older valid snapshot and does not protect
against a compromised local administrator, disk loss or malicious backup edits.

Interrupted writes may leave private `.hosting-state-*.tmp` files in the dedicated
directory. They are never read as authority. After stopping the controller, an
operator can remove those temporary files; retain the validated `state.json` and
its lock file. Routine failed writes clean up their own temporary files.

## Developer integration

```go
hosts, err := hosting.NewPersistent(hosting.DefaultConfig(), absolutePath)
if err != nil {
    return err
}
defer hosts.Close()
```

`NewService(cfg)` retains its existing memory-only behavior. `Persistent()`
reports the selected storage mode, including after closure or a storage fault;
it is not a health check. `Close()` is idempotent and releases the writer lock.
Construct/configure the persistent service before serving requests and close it
after request handling has stopped. Do not switch storage modes on an active
controller.

The hosting test suite covers successful restart, unchanged expiry and
revocation, ephemeral ticket loss, private file modes, malformed and oversized
snapshots, owner/reference/resource integrity, symlink/FIFO/hard-link rejection,
cross-process exclusion, abrupt process exit, concurrent mutation and failures at
every commit stage. Post-rename directory-sync failure is injected against a real
renamed snapshot to verify that authorization stays closed until restart.
