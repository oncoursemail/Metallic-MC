"""Safety and lifecycle regressions; these tests do not simulate Minecraft login."""
import contextlib
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock
import zipfile

import pilot


class PilotTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="metallic-pilot-test-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / "private"
        self.output = io.StringIO()
        with contextlib.redirect_stdout(self.output):
            pilot.initialize(self.root, pilot.BUNDLE / "profile.json", pilot.BUNDLE / "artifacts.lock.json")
        _, self.profile, self.lock = pilot.runtime(self.root)

    def test_init_never_overwrites_world_or_accepts_eula_or_prints_secret(self):
        secret = (self.root / "proxy/forwarding.secret").read_text().strip()
        self.assertNotIn(secret, self.output.getvalue())
        self.assertFalse(pilot.eula_accepted(self.root))
        self.assertFalse(any(self.root.rglob("*.jar")))
        world = self.root / "game/world"
        world.mkdir()
        (world / "precious.txt").write_text("retain me")
        with self.assertRaises(pilot.PilotError):
            pilot.initialize(self.root, pilot.BUNDLE / "profile.json", pilot.BUNDLE / "artifacts.lock.json")
        self.assertEqual((world / "precious.txt").read_text(), "retain me")

    def test_refuses_secrets_in_repository_and_symlink_destination(self):
        with self.assertRaises(pilot.PilotError):
            pilot.initialize(pilot.REPO / "pilot-runtime", pilot.BUNDLE / "profile.json", pilot.BUNDLE / "artifacts.lock.json")
        link = self.base / "link"
        link.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(pilot.PilotError):
            pilot.runtime(link)

    def test_eula_gate_precedes_downloaded_code_execution(self):
        with mock.patch.object(pilot, "java_executable") as java, mock.patch.object(pilot.subprocess, "Popen") as spawn:
            with self.assertRaisesRegex(pilot.PilotError, "EULA"):
                pilot.run(self.root, self.profile, self.lock, "java")
            java.assert_not_called()
            spawn.assert_not_called()

    def test_accepted_eula_does_not_launch_known_unplayable_loopback_nethernet(self):
        for role in ("hub", "game"):
            (self.root / role / "eula.txt").write_text("eula=true\n")  # disposable fixture only
        with mock.patch.object(pilot, "java_executable") as java, mock.patch.object(pilot.subprocess, "Popen") as spawn:
            with self.assertRaisesRegex(pilot.PilotError, "NetherNet.*loopback"):
                pilot.run(self.root, self.profile, self.lock, "java")
            with contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(pilot.main(["preflight", "--root", str(self.root)]), 2)
            java.assert_not_called()
            spawn.assert_not_called()
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            self.assertEqual(pilot.main(["validate", "--root", str(self.root)]), 0)
        result = json.loads(out.getvalue())
        self.assertEqual(result["config"], "valid")
        self.assertEqual(result["eula"], "accepted by owner")
        self.assertFalse(result["launch_ready"])
        self.assertTrue(result["launch_blockers"])

    def test_all_forwarding_and_exposure_changes_fail_closed_without_secret(self):
        cases = [
            ("proxy/velocity.toml", 'bind = "127.0.0.1:', 'bind = "0.0.0.0:'),
            ("proxy/velocity.toml", "online-mode = true", "online-mode = false"),
            ("hub/server.properties", "enable-rcon=false", "enable-rcon=true"),
            ("game/server.properties", "server-ip=127.0.0.1", "server-ip="),
            ("proxy/plugins/Geyser-Velocity/config.yml", 'mode: "builtin"', 'mode: "hybrid"'),
            ("proxy/plugins/Geyser-Velocity/config.yml", "validate-bedrock-login: true", "validate-bedrock-login: false"),
            ("proxy/plugins/Geyser-Velocity/config.yml", "webrtc-port: 19132", "webrtc-port: 19133"),
        ]
        for relative, old, new in cases:
            with self.subTest(relative=relative, setting=old):
                path = self.root / relative
                original = path.read_text()
                path.write_text(original.replace(old, new))
                try:
                    with self.assertRaises(pilot.PilotError):
                        pilot.validate_config(self.root, self.profile)
                finally:
                    path.write_text(original)
        secret = (self.root / "proxy/forwarding.secret").read_text().strip()
        path = self.root / "hub/config/paper-global.yml"
        path.write_text(path.read_text().replace(secret, "f" * 64))
        with self.assertRaises(pilot.PilotError) as error:
            pilot.validate_config(self.root, self.profile)
        self.assertNotIn(secret, str(error.exception))
        self.assertNotIn("f" * 64, str(error.exception))

    def test_unknown_plugins_and_yaml_merges_are_rejected(self):
        (self.root / "hub/plugins/unreviewed.jar").write_bytes(b"unreviewed")
        with self.assertRaisesRegex(pilot.PilotError, "plugin"):
            pilot.validate_config(self.root, self.profile)
        (self.root / "hub/plugins/unreviewed.jar").unlink()
        update = self.root / "hub/plugins/update"
        update.mkdir()
        (update / "unreviewed.jar").write_bytes(b"unreviewed")
        with self.assertRaisesRegex(pilot.PilotError, "plugin"):
            pilot.validate_config(self.root, self.profile)
        (update / "unreviewed.jar").unlink()
        path = self.root / "hub/config/paper-global.yml"
        path.write_text(path.read_text() + "<<: *unsafe\n")
        with self.assertRaises(pilot.PilotError):
            pilot.validate_config(self.root, self.profile)

    def test_upstream_block_list_and_comments_are_understood(self):
        path = self.root / "proxy/plugins/Geyser-Velocity/config.yml"
        source = path.read_text().replace('advertise-addresses: ["127.0.0.1:19132"]', "advertise-addresses:\n      - '127.0.0.1:19132'")
        path.write_text(source)
        pilot.validate_config(self.root, self.profile)
        paper = self.root / "hub/config/paper-global.yml"
        paper.write_text(paper.read_text() + "packet-limiter:\n  overrides:\n    minecraft:place_recipe:\n      action: DROP\n")
        pilot.validate_config(self.root, self.profile)

    def test_download_hash_mismatch_never_installs_or_preserves_partial(self):
        artifact = copy.deepcopy(self.lock["artifacts"][2])
        destination = self.base / "download.jar"
        data = io.BytesIO()
        with zipfile.ZipFile(data, "w") as jar:
            jar.writestr("META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n")
        body = data.getvalue()

        class Response(io.BytesIO):
            status = 200
            def geturl(self): return pilot.artifact_url(artifact)

        opener = mock.Mock()
        opener.open.side_effect = lambda *a, **k: Response(body)
        with self.assertRaisesRegex(pilot.PilotError, "checksum"):
            pilot.acquire(destination, artifact, opener)
        self.assertFalse(destination.exists())
        self.assertFalse(list(self.base.glob("*.part")))
        artifact["sha256"] = hashlib.sha256(body).hexdigest()
        pilot.acquire(destination, artifact, opener)
        pilot.verify_artifact(destination, artifact)
        destination.write_bytes(b"tampered")
        opener.reset_mock()
        with self.assertRaises(pilot.PilotError):
            pilot.acquire(destination, artifact, opener)
        opener.open.assert_not_called()

    def test_artifact_url_cannot_be_injected_and_redirect_is_rejected(self):
        artifact = copy.deepcopy(self.lock["artifacts"][0])
        artifact["version"] = "26.3/../../anything"
        with self.assertRaises(pilot.PilotError):
            pilot.artifact_url(artifact)
        with self.assertRaises(pilot.PilotError):
            pilot.NoRedirects().redirect_request(None, None, 302, "", {}, "https://untrusted.invalid/server.jar")

    def test_tcp_and_udp_collisions_block_admission(self):
        for kind in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
            with self.subTest(kind=kind), socket.socket(socket.AF_INET, kind) as occupied:
                occupied.bind(("127.0.0.1", 0))
                profile = copy.deepcopy(self.profile)
                profile["ports"]["bedrock"] = occupied.getsockname()[1]
                with self.assertRaises(pilot.PilotError):
                    pilot.check_ports(profile)

    def test_shared_memory_and_cpu_budgets_are_enforced(self):
        with mock.patch.object(pilot, "available_resources", return_value=(8191, 4)):
            with self.assertRaisesRegex(pilot.PilotError, "8192 MiB"):
                pilot.check_headroom(self.root, self.profile)
        with mock.patch.object(pilot, "available_resources", return_value=(20000, 2)):
            with self.assertRaisesRegex(pilot.PilotError, "CPU"):
                pilot.check_headroom(self.root, self.profile)

    def test_injected_jvm_options_and_secret_override_are_removed(self):
        with mock.patch.dict(os.environ, {"JAVA_TOOL_OPTIONS": "-javaagent:untrusted.jar", "VELOCITY_FORWARDING_SECRET": "invalid"}):
            env = pilot.clean_java_environment()
        self.assertNotIn("JAVA_TOOL_OPTIONS", env)
        self.assertNotIn("VELOCITY_FORWARDING_SECRET", env)

    def test_stop_uses_console_commands_for_owned_child_handles(self):
        children = {}
        script = "import pathlib,sys; line=sys.stdin.readline(); pathlib.Path(sys.argv[1]).write_text(line)"
        for role in ("hub", "game", "proxy"):
            children[role] = subprocess.Popen([sys.executable, "-c", script, str(self.base / (role + ".stopped"))], stdin=subprocess.PIPE)
        self.assertTrue(pilot.stop_children(children, 5))
        self.assertEqual((self.base / "proxy.stopped").read_text(), "shutdown\n")
        self.assertEqual((self.base / "hub.stopped").read_text(), "stop\n")
        self.assertEqual((self.base / "game.stopped").read_text(), "stop\n")

    def test_broken_child_stdin_does_not_abort_cleanup_of_other_children(self):
        broken = subprocess.Popen([sys.executable, "-c", "import os,time; os.close(0); print('ready', flush=True); time.sleep(0.3)"],
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE)
        self.assertEqual(broken.stdout.readline().strip(), b"ready")
        marker = self.base / "healthy-stopped"
        healthy = subprocess.Popen([sys.executable, "-c", "import pathlib,sys; pathlib.Path(sys.argv[1]).write_text(sys.stdin.readline())", str(marker)],
                                   stdin=subprocess.PIPE)
        self.assertFalse(pilot.stop_children({"hub": broken, "game": healthy}, 2))
        broken.stdout.close()
        self.assertIsNotNone(broken.poll())
        self.assertEqual(healthy.poll(), 0)
        self.assertEqual(marker.read_text(), "stop\n")

    def test_startup_dependency_failure_stops_started_backend_and_never_opens_proxy(self):
        for role in ("hub", "game"):
            (self.root / role / "eula.txt").write_text("eula=true\n")  # test fixture, never a real EULA decision
        real_popen = subprocess.Popen
        script = "import pathlib,sys; role=sys.argv[1];\nif role == 'game': sys.exit(7)\nline=sys.stdin.readline(); pathlib.Path('stopped-test').write_text(line)"
        started = []

        def spawn(args, **kwargs):
            role = kwargs["cwd"].name
            started.append(role)
            return real_popen([sys.executable, "-c", script, role], **kwargs)

        def status(port):
            if port == self.profile["ports"]["game"]:
                raise OSError("fixture not ready")
            return {"online": 0, "protocol": 0}

        with mock.patch.object(pilot, "require_transport_launchable"), mock.patch.object(pilot, "verify_all"), mock.patch.object(pilot.os, "geteuid", return_value=1000), mock.patch.object(pilot, "java_executable", return_value=sys.executable), \
             mock.patch.object(pilot, "check_headroom"), mock.patch.object(pilot, "java_status", side_effect=status), \
             mock.patch.object(pilot.subprocess, "Popen", side_effect=spawn), contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(pilot.PilotError, "JVM exited"):
                pilot.run(self.root, self.profile, self.lock, "java")
        self.assertEqual(started, ["hub", "game"])
        self.assertEqual((self.root / "hub/stopped-test").read_text(), "stop\n")
        with self.assertRaisesRegex(pilot.PilotError, "No active"):
            pilot.request_stop(self.root)

    def test_whitelist_requires_identity_and_never_grants_operator(self):
        with contextlib.redirect_stdout(io.StringIO()):
            pilot.allow_player(self.root, "TestPlayer", "00000000-0000-4000-8000-000000000001")
        for role in ("hub", "game"):
            self.assertEqual(len(json.loads((self.root / role / "whitelist.json").read_text())), 1)
            self.assertFalse((self.root / role / "ops.json").exists())
        with self.assertRaises(pilot.PilotError):
            pilot.allow_player(self.root, "bad\nstop", "invalid")

    def test_stopped_backup_roundtrip_preserves_world_and_secret_without_jars_or_logs(self):
        world = self.root / "game/world/region"
        world.mkdir(parents=True)
        data = b"world state\x00\xff" * 100
        (world / "r.0.0.mca").write_bytes(data)
        (self.root / "game/console.log").write_text("not needed for a world restore")
        (self.root / "game/server.jar").write_bytes(b"re-fetch this from its pin")
        archive = self.base / "stopped.tar.gz"
        restored = self.base / "restored"
        with contextlib.redirect_stdout(io.StringIO()):
            pilot.backup_stopped(self.root, self.profile, archive)
            pilot.restore_new(archive, restored)
        self.assertEqual((restored / "game/world/region/r.0.0.mca").read_bytes(), data)
        self.assertEqual((restored / "proxy/forwarding.secret").read_bytes(), (self.root / "proxy/forwarding.secret").read_bytes())
        self.assertFalse((restored / "game/server.jar").exists())
        self.assertFalse((restored / "game/console.log").exists())
        self.assertFalse(pilot.eula_accepted(restored))
        with self.assertRaises(pilot.PilotError):
            pilot.restore_new(archive, self.root)
        self.assertEqual((world / "r.0.0.mca").read_bytes(), data)

    def test_backup_refuses_active_supervisor_and_unclean_record(self):
        archive = self.base / "stopped.tar.gz"
        with pilot.lease(self.root):
            with self.assertRaises(pilot.BusyError):
                pilot.backup_stopped(self.root, self.profile, archive)
        pilot.atomic_json(self.root / ".running.json", {"state": "unclean-stop"})
        with self.assertRaisesRegex(pilot.PilotError, "clean stopped"):
            pilot.backup_stopped(self.root, self.profile, archive)
        self.assertFalse(archive.exists())

    def test_backup_refuses_orphan_with_runtime_working_directory(self):
        child = subprocess.Popen([sys.executable, "-c", "import sys; print('ready', flush=True); sys.stdin.read()"],
                                 cwd=self.root / "hub", stdin=subprocess.PIPE, stdout=subprocess.PIPE)
        try:
            self.assertEqual(child.stdout.readline().strip(), b"ready")
            with self.assertRaisesRegex(pilot.PilotError, "process"):
                pilot.backup_stopped(self.root, self.profile, self.base / "busy.tar.gz")
        finally:
            child.stdin.close()
            child.wait(timeout=5)
            child.stdout.close()

    def test_backup_respects_java_style_record_lock_even_with_different_cwd(self):
        world = self.root / "game/world"
        world.mkdir()
        lock = world / "session.lock"
        lock.write_bytes(b"lock")
        script = "import fcntl,sys; f=open(sys.argv[1], 'r+'); fcntl.lockf(f,fcntl.LOCK_EX); print('ready',flush=True); sys.stdin.read()"
        child = subprocess.Popen([sys.executable, "-c", script, str(lock)], stdin=subprocess.PIPE, stdout=subprocess.PIPE)
        try:
            self.assertEqual(child.stdout.readline().strip(), b"ready")
            with self.assertRaisesRegex(pilot.PilotError, "still locked"):
                pilot.backup_stopped(self.root, self.profile, self.base / "locked.tar.gz")
        finally:
            child.stdin.close()
            child.wait(timeout=5)
            child.stdout.close()

    def test_world_lock_remains_held_until_archive_is_complete(self):
        world = self.root / "game/world"
        world.mkdir()
        lock = world / "session.lock"
        lock.write_bytes(b"lock")
        archive = self.base / "locked-throughout.tar.gz"
        real_addfile = tarfile.TarFile.addfile
        checked = []
        script = "import fcntl,sys; f=open(sys.argv[1],'r+');\ntry: fcntl.lockf(f,fcntl.LOCK_EX|fcntl.LOCK_NB)\nexcept BlockingIOError: sys.exit(0)\nsys.exit(3)"

        def addfile(stream, member, fileobj=None):
            if member.name == ".pilot-backup-manifest.json":
                result = subprocess.run([sys.executable, "-c", script, str(lock)], capture_output=True, timeout=5)
                self.assertEqual(result.returncode, 0, "world lock was released before the archive finished")
                checked.append(True)
            return real_addfile(stream, member, fileobj)

        with mock.patch.object(tarfile.TarFile, "addfile", addfile), contextlib.redirect_stdout(io.StringIO()):
            pilot.backup_stopped(self.root, self.profile, archive)
        self.assertEqual(checked, [True])
        with tarfile.open(archive) as stream:
            self.assertFalse(any(Path(item.name).name == "session.lock" for item in stream))
        result = subprocess.run([sys.executable, "-c", script, str(lock)], capture_output=True, timeout=5)
        self.assertEqual(result.returncode, 3, "world lock was not released after snapshot completion")

    def test_backup_refuses_hard_link_alias_of_world_lock(self):
        world = self.root / "game/world"
        world.mkdir()
        lock = world / "session.lock"
        lock.write_bytes(b"lock")
        os.link(lock, world / "session.alias")
        archive = self.base / "hardlinked-lock.tar.gz"
        with self.assertRaisesRegex(pilot.PilotError, "World locks.*hard links"):
            pilot.backup_stopped(self.root, self.profile, archive)
        self.assertFalse(archive.exists())

    def test_backup_refuses_hard_linked_source_file(self):
        external = self.base / "external.txt"
        external.write_text("must not enter the backup through another name")
        os.link(external, self.root / "game/external.alias")
        archive = self.base / "hardlinked-source.tar.gz"
        with self.assertRaisesRegex(pilot.PilotError, "Backup source files.*hard links"):
            pilot.backup_stopped(self.root, self.profile, archive)
        self.assertFalse(archive.exists())
        self.assertTrue(external.exists())

    def test_backup_refuses_nonregular_world_lock(self):
        world = self.root / "game/world"
        world.mkdir()
        os.mkfifo(world / "session.lock", 0o600)
        archive = self.base / "nonregular-lock.tar.gz"
        with self.assertRaisesRegex(pilot.PilotError, "World locks must be regular"):
            pilot.backup_stopped(self.root, self.profile, archive)
        self.assertFalse(archive.exists())

    def test_failed_checksum_write_cleans_only_owned_files_and_allows_retry(self):
        archive = self.base / "retry.tar.gz"
        sidecar = archive.with_name(archive.name + ".sha256.json")
        real_write = pilot.write_new

        def fail_checksum(path, content):
            if path.name.startswith(sidecar.name + "."):
                real_write(path, b"{")
                raise OSError("injected checksum write failure")
            return real_write(path, content)

        with mock.patch.object(pilot, "write_new", fail_checksum):
            with self.assertRaisesRegex(OSError, "checksum write failure"):
                pilot.backup_stopped(self.root, self.profile, archive)
        self.assertFalse(archive.exists())
        self.assertFalse(sidecar.exists())
        self.assertEqual(list(self.base.glob("*.part")), [])
        self.assertTrue(self.root.exists())
        with contextlib.redirect_stdout(io.StringIO()):
            pilot.backup_stopped(self.root, self.profile, archive)
            pilot.restore_new(archive, self.base / "retried-restore")
        self.assertTrue(sidecar.exists())

    def test_snapshot_at_exact_file_count_limit_can_be_restored(self):
        count = sum(path.is_file() and not pilot.snapshot_excluded(path.relative_to(self.root).as_posix())
                    for path in self.root.rglob("*"))
        archive = self.base / "file-count-boundary.tar.gz"
        restored = self.base / "file-count-restored"
        with mock.patch.object(pilot, "SNAPSHOT_MAX_FILES", count), contextlib.redirect_stdout(io.StringIO()):
            pilot.backup_stopped(self.root, self.profile, archive)
            pilot.restore_new(archive, restored)
        self.assertTrue((restored / "proxy/forwarding.secret").is_file())

    def test_backup_refuses_paths_and_manifest_beyond_restore_limits(self):
        archive = self.base / "over-bound.tar.gz"
        with mock.patch.object(pilot, "SNAPSHOT_MAX_MANIFEST", 16):
            with self.assertRaisesRegex(pilot.PilotError, "manifest exceeds"):
                pilot.backup_stopped(self.root, self.profile, archive)
        self.assertFalse(archive.exists())
        with mock.patch.object(pilot, "SNAPSHOT_MAX_NAME", 8):
            with self.assertRaisesRegex(pilot.PilotError, "path cannot be restored"):
                pilot.backup_stopped(self.root, self.profile, archive)
        self.assertFalse(archive.exists())
        (self.root / "game/unrestorable\\name").write_text("invalid archive path")
        with self.assertRaisesRegex(pilot.PilotError, "path cannot be restored"):
            pilot.backup_stopped(self.root, self.profile, archive)
        self.assertFalse(archive.exists())
        self.assertFalse(archive.with_name(archive.name + ".sha256.json").exists())

    def test_restore_detects_corruption_before_creating_destination(self):
        archive = self.base / "stopped.tar.gz"
        with contextlib.redirect_stdout(io.StringIO()):
            pilot.backup_stopped(self.root, self.profile, archive)
        data = bytearray(archive.read_bytes())
        data[len(data) // 2] ^= 1
        archive.write_bytes(data)
        destination = self.base / "must-not-exist"
        with self.assertRaisesRegex(pilot.PilotError, "checksum"):
            pilot.restore_new(archive, destination)
        self.assertFalse(destination.exists())

    def test_restore_refuses_tar_path_traversal_and_removes_only_partial_destination(self):
        archive = self.base / "malformed.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            item = tarfile.TarInfo("../escaped.txt")
            item.size = 4
            stream.addfile(item, io.BytesIO(b"evil"))
        pilot.write_new(archive.with_name(archive.name + ".sha256.json"), json.dumps({
            "schema": 1, "sha256": hashlib.sha256(archive.read_bytes()).hexdigest(), "size": archive.stat().st_size}))
        destination = self.base / "must-not-exist"
        with self.assertRaisesRegex(pilot.PilotError, "Unsafe backup path"):
            pilot.restore_new(archive, destination)
        self.assertFalse(destination.exists())
        self.assertFalse((self.base / "escaped.txt").exists())
        self.assertTrue(self.root.exists())


if __name__ == "__main__":
    unittest.main()
