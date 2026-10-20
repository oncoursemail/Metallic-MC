# Private controller and telemetry recovery with systemd

This opt-in deployment runs the **Metallic-MC controller and read-only telemetry agent** under the system systemd manager. It works without a login session or a user systemd bus. It does not install or launch Minecraft, accept an EULA, open a firewall, configure a domain, manage Docker, or operate Pterodactyl/Wings. No host installation is implied by the presence of these files.

## Required project isolation for this installation

The owner's deployment requirement is that **Metallic and Metallic-MC remain in separate containers**. This systemd bundle is an optional implementation for a dedicated Linux environment; installing it directly on a shared host does not provide or replace that project container boundary. For the current Brain1 installation, a compatible container/runtime environment still needs to be set up before deploying this bundle. Source files and prepared binaries can be staged for review in the meantime. No container stack or runtime installation is included here.

## What is prepared

`infra/systemd/deploy.py prepare` creates a new private bundle from **two local binaries and their explicitly supplied SHA-256 pins**. It performs no downloads. Both binaries are verified before preparation and after copying. Review the source commit and build/test evidence before accepting a pin; a checksum provides integrity, not independent publisher authentication.

The bundle contains two unit files, a release manifest, the binaries, and separate `controller.env` and `agent.env` files. The generated administrator password appears only in `controller.env`; the telemetry account receives only its shared heartbeat token. Do not paste either environment file into chat, CI output, or logs. Keep the complete generated bundle private and outside Git. Architect defaults OFF; passing `--architect` explicitly opts into the private workbench and durable host registry on the **first installation**. Updates preserve the existing configuration and do not change that choice.

## Prepare and review

Requirements: Python 3.11+, Linux, a running **system** systemd manager version 249 or later, cgroup v2, `useradd`, and built Linux binaries of the host's architecture. Use the project's tested Go toolchain to build `cmd/metallic-control` and `cmd/metallic-agent`. Run `sha256sum` on the reviewed artifacts, then substitute those exact 64-character values below.

```bash
python3 infra/systemd/deploy.py prepare \
  --control /absolute/path/metallic-control \
  --control-sha256 CONTROL_SHA256_FROM_REVIEWED_BUILD \
  --agent /absolute/path/metallic-agent \
  --agent-sha256 AGENT_SHA256_FROM_REVIEWED_BUILD \
  --output /absolute/private/new-metallic-service-bundle \
  --port 8080 --node-id brain1
```

For an explicitly enabled Architect installation, add `--architect`. There is no public-bind option. Review the manifest, unit files and generated configuration locally. The script only supports unquoted `KEY=value` environment entries; keep secret values single-line. Generated values are hexadecimal and require no escaping.

The prepared unit paths refer to the final installation. `systemd-analyze verify` on an uninstalled host may complain that the final executable does not exist. Verify syntax against an isolated temporary copy with its `ExecStart` replaced by a known local executable; never change the deployment copy to a different executable merely to silence that warning.

## Explicit installation

Install first without starting anything:

```bash
sudo python3 infra/systemd/deploy.py install \
  --bundle /absolute/private/new-metallic-service-bundle
```

Installation creates `metallic-control` and `metallic-metrics` as separate system accounts with no home directory or login shell. Existing unrelated accounts, unknown service units, local service overrides, unexpected directories, mismatched checksums, and port collisions stop installation. Hosts with a Pterodactyl configuration directory or active `wings.service` are refused. The installer never stops the owner of an occupied port. During an update, it stops only its own previously registered units before checking the port.

The initial administrator password remains in `/etc/metallic-mc/controller.env`, readable by root. systemd reads the root-owned environment files before dropping each process to its service account. Both files are mode `0600` in a root-owned `0700` directory. The durable host store is owned only by `metallic-control`, at `/var/lib/metallic-mc/controller/hosting`, mode `0700`. The metrics user receives no write access to controller state and no Docker group membership.

After reviewing the installation, this explicitly enables startup at boot and starts both units:

```bash
sudo systemctl enable --now metallic-mc-control.service metallic-mc-agent.service
```

Alternatively, add `--enable-and-start` to the installer command when that complete action is approved. Opening a service port or public domain remains a separate task. Access the UI at `http://127.0.0.1:8080` locally or through an authenticated SSH tunnel; the generated cookies are suitable for this local HTTP setup. HTTPS proxy/Discord configuration requires a separate review of secure-cookie and origin settings.

## Recovery behavior and limits

| Behavior | Controller | Telemetry agent |
| --- | --- | --- |
| Account | `metallic-control` | `metallic-metrics` |
| Restart policy | Failure restart after 15 seconds | Failure restart after 15 seconds |
| Repeated failures | At most 5 starts per 300 seconds | At most 5 starts per 300 seconds |
| Stop deadline | SIGTERM, 30 seconds, then process-group cleanup | SIGTERM, 15 seconds, then process-group cleanup |
| Memory high / maximum | 192 / 256 MiB | 48 / 64 MiB |
| CPU quota | 50% of one CPU | 10% of one CPU |
| Swap allowance | 0 | 0 |
| Network | Controller binds loopback; outbound catalog/Discord HTTPS remains possible | Loopback destination filter and pinned controller URL |

These are modest initial service budgets, not Minecraft capacity reservations. Verify actual consumption before tuning them. Five repeated startup failures stop the retry loop; after fixing the cause, use `systemctl reset-failed` followed by `start`. The fixed 15-second delay deliberately works on the stated baseline instead of requiring newer exponential-backoff settings. The controller drains HTTP requests for up to 15 seconds internally, within systemd's 30-second stop window. The telemetry process has no mutation to flush and continues retrying failed heartbeat delivery while alive.

The telemetry agent observes `/proc` and disk statistics but cannot write them. `ProtectProc=invisible` and private network namespaces are intentionally absent: they would hide host observations or break communication with the controller. The agent's IP filter requires kernel/systemd cgroup-BPF support; inspect systemd's journal for unsupported-filter warnings. Its configured destination remains literal loopback even if that defense is unavailable.

Check the real outcome after starting:

```bash
systemctl is-active metallic-mc-control.service metallic-mc-agent.service
curl --fail --max-time 3 http://127.0.0.1:8080/healthz
sudo journalctl -u metallic-mc-control.service -u metallic-mc-agent.service --since '5 minutes ago' --no-pager
```

Also sign in and verify that the agent's authenticated heartbeat produces a fresh node. A successful `/healthz` alone does not prove telemetry, a healthy durable store, game availability, Minecraft login, or backups. Confirm persistent registry recovery with an approved test host before relying on it. Pending pairing tickets, browser sessions, and old live telemetry do not survive controller restart; revoked host credentials remain revoked in durable mode.

## Updates and rollback

Prepare a new bundle with newly reviewed pins, then invoke `install` again. Existing environment files and state are preserved. Unit contents must still match; changes to unit policy need their own reviewed deployment rather than silent overwrite. The installer keeps binaries under `/opt/metallic-mc/releases/RELEASE`, atomically selects `current`, and saves the prior release as `previous`. Old releases are retained, never deleted automatically. Without `--enable-and-start`, an update leaves these services stopped for inspection.

To select the previous binaries:

```bash
sudo python3 infra/systemd/deploy.py rollback
```

Review compatibility, then start both units, or explicitly use `rollback --start`. Rollback rechecks the retained binary digests. It does **not** roll back secrets, unit configuration, identity records, or host-state schemas. Never restore an old host registry to roll back application code: doing so could revive revoked authority. Confirm that the older release supports the current durable-store schema. After rollback, `previous` points at the version just replaced, allowing a reviewed return.

Installation is not a filesystem-wide transaction. If an account-creation, filesystem, or systemd error interrupts installation, it fails with existing data preserved and may leave a partial installation or stopped managed services. Inspect the reported step and actual files; do not delete state or relax directory checks to force a retry. No automatic rollback is attempted after a failed start because that could conceal a schema incompatibility. Keep a stopped-service backup and test its restoration separately; this bundle does not claim to provide off-host or immutable backups.

## Local verification and sources

Run `python3 -m unittest discover -s infra/systemd -p '*_test.py'`. These regressions verify private secret separation, output permissions under a restrictive umask, default-off Architect, explicit durable paths, checksum rejection, public endpoint rejection, symlink rejection, and preservation of existing bundle files. They do not perform privileged host installation or prove restart after a real power loss.

Official reference material used for the service design:

- [systemd.service](https://www.freedesktop.org/software/systemd/man/systemd.service.html): restart delays, start limits and stop deadlines.
- [systemd.exec](https://www.freedesktop.org/software/systemd/man/systemd.exec.html): environment files, process identities and filesystem restrictions.
- [systemd.resource-control](https://www.freedesktop.org/software/systemd/man/systemd.resource-control.html): memory, CPU, task and network cgroup controls.
- [systemd.unit](https://www.freedesktop.org/software/systemd/man/systemd.unit.html): unit start-rate limits.
