#!/bin/sh
# Run a command in its own PID namespace with a fresh /proc, so it sees only its own processes.
#
# tools/pilot.py refuses a stopped snapshot when it cannot read the working directory of some
# same-user process (live_runtime_processes). A CI runner, or any host where the user is logged
# in over ssh, always has such processes (sshd, systemd --user, sd-pam: not dumpable), so the
# pilot tests fail there before reaching what they test. Inside its own namespace the suite sees
# every process it could have started, so the check runs in full and nothing is skipped.
#
# An unprivileged user namespace first; where the host forbids that (Ubuntu 24.04's AppArmor
# default, as on GitHub's runners) sudo makes the namespaces and setpriv drops back to the
# calling user before the command starts.
set -eu
if unshare --user --map-current-user --pid --fork --mount-proc true 2>/dev/null; then
  exec unshare --user --map-current-user --pid --fork --mount-proc "$@"
fi
exec sudo -n -E unshare --pid --fork --mount-proc \
  setpriv --reuid="$(id -u)" --regid="$(id -g)" --init-groups env "PATH=$PATH" "$@"
