#!/usr/bin/env python3
"""Metallic-MC's explicit, unprivileged, loopback-only Minecraft pilot.

Python 3.11+; run/stop require Linux. Nothing accepts the Minecraft EULA,
installs Java, downloads an unpinned URL, or manages system services.
"""
from __future__ import annotations

import argparse
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
from string import Template
import tarfile
import time
import tomllib
import urllib.error
import urllib.request
import uuid
import zipfile

REPO = Path(__file__).resolve().parents[1]
BUNDLE = REPO / "infra" / "pilot"
MIB = 1024 * 1024
MAX_ARTIFACT = 200 * MIB
SNAPSHOT_MAX_FILES = 100000
SNAPSHOT_MAX_NAME = 4096
SNAPSHOT_MAX_MANIFEST = 32 * MIB
USER_AGENT = "Metallic-MC-private-pilot/0.1 (https://metallic.club)"
DESTINATIONS = {
    "paper": ("hub/server.jar", "game/server.jar"),
    "velocity": ("proxy/server.jar",),
    "geyser": ("proxy/plugins/Geyser-Velocity.jar",),
    "floodgate": ("proxy/plugins/floodgate-velocity.jar",),
}


class PilotError(Exception):
    pass


class BusyError(PilotError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise PilotError(message)


def safe_path(path: Path) -> Path:
    path = Path(os.path.abspath(path.expanduser()))
    for part in (path, *path.parents):
        require(not part.is_symlink(), "Symlinks are not permitted in pilot paths")
    return path


def read_json(path: Path):
    safe_path(path)
    require(path.stat().st_size <= MIB, f"JSON file is too large: {path.name}")
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (UnicodeError, ValueError) as exc:
        raise PilotError(f"Invalid JSON: {path.name}") from exc


def write_new(path: Path, content: str | bytes) -> None:
    safe_path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
    with os.fdopen(os.open(path, flags, 0o600), "wb") as stream:
        stream.write(content.encode("utf-8") if isinstance(content, str) else content)
        stream.flush()
        os.fsync(stream.fileno())


def atomic_json(path: Path, data) -> None:
    safe_path(path)
    temporary = path.with_name(path.name + "." + secrets.token_hex(8) + ".tmp")
    try:
        write_new(temporary, json.dumps(data, indent=2) + "\n")
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def load_profile(path: Path) -> dict:
    p = read_json(path)
    require(p.get("schema") == 1 and p.get("bind") == "127.0.0.1", "Only the loopback pilot profile is supported")
    ports = p.get("ports", {})
    require(set(ports) == {"proxy", "hub", "game", "bedrock"}, "Four explicit pilot ports are required")
    require(all(type(x) is int and 1024 <= x <= 65535 for x in ports.values()), "Ports must be unprivileged integers")
    require(len(set(ports.values())) == 4, "Pilot ports must be distinct, including the TCP+UDP Bedrock port")
    heaps = p.get("heap_mib", {})
    require(set(heaps) == {"proxy", "hub", "game"}, "Every JVM needs a heap limit")
    require(all(type(x) is int and 512 <= x <= 4096 for x in heaps.values()) and sum(heaps.values()) <= 6144, "Pilot heap budget must be between 512 MiB per JVM and 6144 MiB total")
    bounds = {"native_budget_mib": (1536, 8192), "shared_reserve_mib": (2048, 131072),
              "minimum_cpus": (4, 1024), "minimum_disk_free_mib": (10240, 1048576),
              "stop_disk_free_mib": (2048, 1048576), "runtime_disk_budget_mib": (1024, 10240),
              "players_per_backend": (1, 12), "startup_timeout_seconds": (30, 300),
              "shutdown_timeout_seconds": (10, 60)}
    for key, (low, high) in bounds.items():
        require(type(p.get(key)) is int and low <= p[key] <= high, f"Invalid resource bound: {key}")
    require(p["stop_disk_free_mib"] < p["minimum_disk_free_mib"], "Disk start threshold must exceed the stop threshold")
    return p


def artifact_url(a: dict) -> str:
    ident = a.get("id")
    require(ident in DESTINATIONS and a.get("project") == ident, "Unknown artifact project")
    require(isinstance(a.get("version"), str) and re.fullmatch(r"[0-9]+(?:\.[0-9]+){1,3}", a["version"]), "Artifacts require an exact release version")
    require(type(a.get("build")) is int and a["build"] > 0, "Artifacts require an exact build")
    require(isinstance(a.get("sha256"), str) and re.fullmatch(r"[0-9a-f]{64}", a["sha256"]), "Artifacts require a SHA-256 pin")
    if ident in ("paper", "velocity"):
        name = f"{ident}-{a['version']}-{a['build']}.jar"
        require(a.get("channel") == "STABLE" and a.get("name") == name, "PaperMC artifacts must be stable builds with exact names")
        require(type(a.get("size")) is int and 0 < a["size"] <= MAX_ARTIFACT, "PaperMC artifact size must be pinned")
        return f"https://fill-data.papermc.io/v1/objects/{a['sha256']}/{name}"
    name = "Geyser-Velocity.jar" if ident == "geyser" else "floodgate-velocity.jar"
    require(a.get("name") == name and a.get("channel") == "default", "Only official Velocity bridge artifacts are supported")
    return f"https://download.geysermc.org/v2/projects/{ident}/versions/{a['version']}/builds/{a['build']}/downloads/velocity"


def load_lock(path: Path) -> dict:
    lock = read_json(path)
    require(lock.get("schema") == 1 and lock.get("java_major") == 25, "Unsupported artifact lock or Java baseline")
    artifacts = lock.get("artifacts", [])
    require(isinstance(artifacts, list) and len(artifacts) == 4 and all(isinstance(a, dict) for a in artifacts)
            and {a.get("id") for a in artifacts} == set(DESTINATIONS), "Exactly four pinned upstream artifacts are required")
    for artifact in artifacts:
        artifact_url(artifact)
    return lock


def runtime(path: Path) -> tuple[Path, dict, dict]:
    root = safe_path(path)
    require(root.is_dir() and not root.is_relative_to(REPO), "The runtime must be a private directory outside the repository")
    require(read_json(root / "pilot.json").get("kind") == "metallic-mc-private-pilot", "Not a Metallic-MC pilot directory")
    if os.name == "posix":
        require(not root.stat().st_mode & 0o077, "Pilot directory permissions must be 0700")
        require(root.stat().st_uid == os.getuid(), "The pilot must be owned by the current user")
    return root, load_profile(root / "profile.json"), load_lock(root / "artifacts.lock.json")


@contextlib.contextmanager
def lease(root: Path):
    require(sys.platform.startswith("linux"), "Lifecycle and artifact mutations currently require Linux; no Windows process control is implemented")
    import fcntl
    path = safe_path(root / ".supervisor.lock")
    fd = os.open(path, os.O_RDWR | os.O_CREAT | getattr(os, "O_NOFOLLOW", 0), 0o600)
    try:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise BusyError("Another pilot operation or supervisor holds the runtime lock") from exc
        yield
    finally:
        os.close(fd)


def initialize(root: Path, profile_path: Path, lock_path: Path) -> None:
    root = safe_path(root)
    require(not root.is_relative_to(REPO), "Runtime files and secrets must be outside the repository")
    require(not root.exists(), "Refusing to overwrite an existing directory or world; choose a new pilot directory")
    profile, lock = load_profile(profile_path), load_lock(lock_path)
    root.mkdir(mode=0o700, parents=False)
    secret = secrets.token_hex(32)
    variables = {f"{key}_port": value for key, value in profile["ports"].items()}
    variables.update(forwarding_secret=secret, metrics_uuid=str(uuid.uuid4()), display_players=2 * profile["players_per_backend"])

    def render(template: str, destination: str, extra: dict | None = None):
        text = (BUNDLE / "templates" / template).read_text(encoding="utf-8")
        write_new(root / destination, Template(text).substitute(variables | (extra or {})))

    write_new(root / "pilot.json", json.dumps({"kind": "metallic-mc-private-pilot", "schema": 1}) + "\n")
    write_new(root / "profile.json", json.dumps(profile, indent=2) + "\n")
    write_new(root / "artifacts.lock.json", json.dumps(lock, indent=2) + "\n")
    render("velocity.toml.in", "proxy/velocity.toml")
    render("geyser.yml.in", "proxy/plugins/Geyser-Velocity/config.yml")
    render("floodgate.yml.in", "proxy/plugins/floodgate/config.yml")
    write_new(root / "proxy/forwarding.secret", secret + "\n")
    for role in ("hub", "game"):
        render("paper-global.yml.in", f"{role}/config/paper-global.yml")
        render("server.properties.in", f"{role}/server.properties", {
            "role": role, "port": profile["ports"][role], "players": profile["players_per_backend"],
            "gamemode": "adventure" if role == "hub" else "survival",
            "level_type": "minecraft:flat" if role == "hub" else "minecraft:normal",
        })
        write_new(root / role / "eula.txt", "# Review https://www.minecraft.net/eula before accepting.\neula=false\n")
        write_new(root / role / "spigot.yml", "settings:\n  bungeecord: false\n")
        write_new(root / role / "bukkit.yml", "settings:\n  allow-end: false\n")
        write_new(root / role / "whitelist.json", "[]\n")
        (root / role / "plugins").mkdir(mode=0o700)
    for role in ("hub", "game", "proxy"):
        write_new(root / role / "plugins/bStats/config.yml", f"enabled: false\nserverUuid: \"{uuid.uuid4()}\"\nlogFailedRequests: false\nlogSentData: false\nlogResponseStatusText: false\n")
    validate_config(root, profile)
    print("Private pilot initialized. Minecraft EULA remains unaccepted; no JARs were downloaded or started.")


class NoRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise PilotError("Artifact redirect refused; review the official lock instead of following a new URL")


def verify_artifact(path: Path, artifact: dict) -> None:
    safe_path(path)
    require(path.is_file() and 0 < path.stat().st_size <= MAX_ARTIFACT, f"Missing or oversized artifact: {artifact['id']}")
    if "size" in artifact:
        require(path.stat().st_size == artifact["size"], f"Artifact size mismatch: {artifact['id']}")
    with path.open("rb") as stream:
        actual = hashlib.file_digest(stream, "sha256").hexdigest()
    require(secrets.compare_digest(actual, artifact["sha256"]), f"Artifact checksum mismatch: {artifact['id']}; file will not run")
    require(zipfile.is_zipfile(path), f"Pinned artifact is not a JAR: {artifact['id']}")


def acquire(path: Path, artifact: dict, opener=None) -> None:
    url = artifact_url(artifact)
    safe_path(path)
    if path.exists():
        verify_artifact(path, artifact)
        return
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_name(path.name + "." + secrets.token_hex(8) + ".part")
    opener = opener or urllib.request.build_opener(NoRedirects())
    try:
        request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT, "Accept": "application/java-archive"})
        with opener.open(request, timeout=30) as response:
            require(response.status == 200 and response.geturl() == url, "Unexpected artifact response or redirect")
            flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
            with os.fdopen(os.open(temporary, flags, 0o600), "wb") as output:
                total = 0
                while block := response.read(MIB):
                    total += len(block)
                    require(total <= min(artifact.get("size", MAX_ARTIFACT), MAX_ARTIFACT), "Artifact exceeded its size budget")
                    output.write(block)
                output.flush()
                os.fsync(output.fileno())
        verify_artifact(temporary, artifact)
        os.link(temporary, path)  # atomic creation; never replace another artifact
    finally:
        temporary.unlink(missing_ok=True)


def fetch(root: Path, profile: dict, lock: dict) -> None:
    with lease(root):
        require(shutil.disk_usage(root).free >= profile["minimum_disk_free_mib"] * MIB, "Insufficient disk headroom for the pilot")
        for artifact in lock["artifacts"]:
            destinations = [root / item for item in DESTINATIONS[artifact["id"]]]
            acquire(destinations[0], artifact)
            for destination in destinations[1:]:
                safe_path(destination)
                if not destination.exists():
                    write_new(destination, destinations[0].read_bytes())
                verify_artifact(destination, artifact)
            print(f"Verified {artifact['id']} {artifact['version']} build {artifact['build']}")
    print("All entrypoint JARs match the lock. Nothing was started.")


def properties(path: Path) -> dict[str, str]:
    result = {}
    for raw in safe_path(path).read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith(("#", "!")):
            continue
        require("=" in line and not line.endswith("\\"), f"Unsupported properties syntax: {path.name}")
        key, value = line.split("=", 1)
        key, value = key.strip(), value.strip()
        require(key not in result and "\\" not in key, f"Duplicate or escaped property key: {path.name}")
        result[key] = value
    return result


def yaml_scalars(path: Path) -> dict[str, object]:
    """Read the simple mapping subset emitted here and by upstream config writers.

    No YAML constructors, anchors, aliases, merges or alternate documents. Complex
    values outside the checked paths remain opaque; security-critical values must
    resolve to their expected scalar types. Unknown syntax fails closed.
    """
    result: dict[str, object] = {}
    stack: list[tuple[int, str]] = []
    seen = set()
    for raw in safe_path(path).read_text(encoding="utf-8").splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        require("\t" not in raw and raw.strip() not in ("---", "..."), f"Unsupported YAML layout: {path.name}")
        indent = len(raw) - len(raw.lstrip())
        line = raw.strip()
        # Upstream may emit block lists for unrelated settings or address arrays.
        if line.startswith("- "):
            while stack and stack[-1][0] > indent:
                stack.pop()
            require(bool(stack), f"Unexpected YAML list: {path.name}")
            key = ".".join(key for _, key in stack)
            item = scalar(line[2:], path.name)
            if key not in result:
                result[key] = []
            require(isinstance(result[key], list), f"Conflicting YAML list: {path.name}")
            result[key].append(item)
            continue
        while stack and stack[-1][0] >= indent:
            stack.pop()
        match = re.fullmatch(r"([A-Za-z0-9_.:-]+):(?:\s+(.*))?", line)
        require(match is not None, f"Unsupported YAML syntax; review {path.name}")
        name, value = match.groups()
        full = ".".join([key for _, key in stack] + [name])
        require(full not in seen, f"Duplicate YAML key: {path.name}")
        seen.add(full)
        if value is None or value.startswith("#"):
            stack.append((indent, name))
        else:
            result[full] = scalar(value, path.name)
    return result


def scalar(value: str, name: str):
    value = value.strip()
    require(not value.startswith(("&", "*", "!", ">", "|")), f"Unsupported YAML value: {name}")
    if value.startswith('"') or value.startswith("["):
        try:
            parsed, end = json.JSONDecoder().raw_decode(value)
            require(not value[end:].strip() or value[end:].strip().startswith("#"), f"Invalid YAML suffix: {name}")
            return parsed
        except ValueError as exc:
            raise PilotError(f"Unsupported quoted YAML value: {name}") from exc
    if value.startswith("'"):
        match = re.fullmatch(r"'((?:[^']|'')*)'(?:\s+#.*)?", value)
        require(match is not None, f"Invalid YAML quote: {name}")
        return match[1].replace("''", "'")
    value = re.split(r"\s+#", value, maxsplit=1)[0]
    if value in ("true", "false"):
        return value == "true"
    if re.fullmatch(r"-?[0-9]+", value):
        return int(value)
    return value


def validate_config(root: Path, profile: dict) -> None:
    ports = profile["ports"]
    with safe_path(root / "proxy/velocity.toml").open("rb") as stream:
        proxy = tomllib.load(stream)
    required_proxy = {"bind": f"127.0.0.1:{ports['proxy']}", "online-mode": True,
                      "player-info-forwarding-mode": "modern", "forwarding-secret-file": "forwarding.secret"}
    for key, expected in required_proxy.items():
        require(proxy.get(key) == expected, f"Unsafe or mismatched Velocity setting: {key}")
    require(proxy.get("query", {}).get("enabled") is False and proxy.get("advanced", {}).get("haproxy-protocol") is False, "Unexpected Velocity query or PROXY protocol listener")
    require(proxy.get("servers") == {"hub": f"127.0.0.1:{ports['hub']}", "game": f"127.0.0.1:{ports['game']}", "try": ["hub"]}, "Velocity backend routing differs from the private pilot")
    require(not proxy.get("forced-hosts"), "Forced public hosts are not part of the private pilot")
    secret_path = safe_path(root / "proxy/forwarding.secret")
    secret = secret_path.read_text(encoding="utf-8").strip()
    require(re.fullmatch(r"[0-9a-f]{64}", secret) is not None, "Invalid forwarding secret; its value is never printed")
    for role in ("hub", "game"):
        config_path = root / role / "config/paper-global.yml"
        if os.name == "posix":
            require(not config_path.stat().st_mode & 0o077 and not secret_path.stat().st_mode & 0o077, "Forwarding-secret files must have mode 0600")
        paper = yaml_scalars(config_path)
        require(paper.get("proxies.velocity.enabled") is True and paper.get("proxies.velocity.online-mode") is True, f"Unsafe forwarding/authentication on {role}")
        require(secrets.compare_digest(str(paper.get("proxies.velocity.secret", "")), secret), f"Forwarding secret mismatch on {role}; values are never printed")
        require(paper.get("proxies.proxy-protocol") is False, f"Unexpected PROXY protocol on {role}")
        settings = properties(root / role / "server.properties")
        expected = {"server-ip": "127.0.0.1", "server-port": str(ports[role]), "online-mode": "false",
                    "enable-rcon": "false", "enable-query": "false", "enable-jmx-monitoring": "false",
                    "management-server-enabled": "false", "white-list": "true", "enforce-whitelist": "true",
                    "level-name": "world", "max-players": str(profile["players_per_backend"])}
        require(all(settings.get(key) == value for key, value in expected.items()), f"Unsafe or mismatched {role} server.properties")
        require(yaml_scalars(root / role / "spigot.yml").get("settings.bungeecord") is False, f"Legacy forwarding must remain disabled on {role}")
        safe_path(root / role / "world")
    geyser = yaml_scalars(root / "proxy/plugins/Geyser-Velocity/config.yml")
    required_geyser = {"bedrock.address": "127.0.0.1", "bedrock.port": ports["bedrock"], "bedrock.webrtc-port": ports["bedrock"], "bedrock.clone-remote-port": False,
                       "bedrock.transport": "nethernet", "bedrock.signaling.mode": "builtin", "java.auth-type": "floodgate",
                       "bedrock.signaling.builtin.advertise-addresses": [f"127.0.0.1:{ports['bedrock']}"],
                       "advanced.bedrock.validate-bedrock-login": True, "advanced.bedrock.use-haproxy-protocol": False,
                       "advanced.java.use-haproxy-protocol": False, "advanced.java.use-direct-connection": True,
                       "advanced.nxs.stun-discovery": False, "advanced.nxs.assisted-joins": False}
    require(all(geyser.get(key) == value for key, value in required_geyser.items()), "Unsafe or mismatched Geyser authentication, binding, or signaling")
    floodgate = yaml_scalars(root / "proxy/plugins/floodgate/config.yml")
    require(floodgate.get("username-prefix") == "." and floodgate.get("player-link.enabled") is False, "Floodgate must retain its distinct names and private-pilot linking policy")
    # Only these reviewed proxy plugins and no backend plugins are managed here.
    for role in ("hub", "game", "proxy"):
        allowed = {"Geyser-Velocity.jar", "floodgate-velocity.jar"} if role == "proxy" else set()
        plugins = safe_path(root / role / "plugins")
        for item in plugins.rglob("*"):
            require(not item.is_symlink(), f"Plugin symlink found on {role}")
        found = {p.relative_to(plugins).as_posix() for p in plugins.rglob("*.jar")}
        require(found <= allowed, f"Unreviewed plugin JARs found on {role}; use a separate integration profile")


def verify_all(root: Path, lock: dict) -> None:
    for artifact in lock["artifacts"]:
        for relative in DESTINATIONS[artifact["id"]]:
            verify_artifact(root / relative, artifact)


def eula_accepted(root: Path) -> bool:
    return all(properties(root / role / "eula.txt").get("eula") == "true" for role in ("hub", "game"))


def transport_launch_blockers(profile: dict) -> list[str]:
    """Static security validation is not proof of a usable NetherNet ICE path.

    The pinned Geyser b1265 source explicitly warns that loopback ICE candidates
    cannot connect, even from a local client. Keep private staging available but
    refuse to execute the known-unplayable profile. A supported non-loopback
    container profile must be implemented and validated before this gate changes.
    """
    if profile.get("bind") == "127.0.0.1":
        return ["Pinned Geyser NetherNet cannot establish gameplay over loopback. A verified private container interface and reachable ICE address are required; the current profile supports staging only."]
    return ["No validated non-loopback private container transport profile is implemented."]


def require_transport_launchable(profile: dict) -> None:
    blockers = transport_launch_blockers(profile)
    require(not blockers, " ".join(blockers))


def clean_java_environment() -> dict[str, str]:
    env = os.environ.copy()
    for key in ("JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "JDK_JAVA_OPTIONS", "CLASSPATH", "VELOCITY_FORWARDING_SECRET"):
        env.pop(key, None)
    return env


def java_executable(command: str) -> str:
    resolved = shutil.which(command)
    require(resolved is not None, "Java 25 is not installed or not on PATH; this tool does not install it")
    result = subprocess.run([resolved, "-version"], capture_output=True, text=True, timeout=15, env=clean_java_environment())
    match = re.search(r'(?:openjdk|java) version "([0-9]+)', result.stderr + result.stdout)
    require(result.returncode == 0 and match is not None and int(match[1]) == 25, "This pinned pilot requires Java 25; select an installed Java 25 executable")
    return str(Path(resolved).resolve())


def available_resources() -> tuple[int, float]:
    require(sys.platform.startswith("linux"), "Live resource admission currently requires Linux")
    memory = properties_from_meminfo(Path("/proc/meminfo").read_text())
    require("MemAvailable" in memory, "Linux available-memory accounting is unavailable")
    available = memory["MemAvailable"] * 1024
    cpus = float(len(os.sched_getaffinity(0)))
    # Include the container's and ancestors' cgroup v2 limits, not just host RAM.
    cgroup_root = Path("/sys/fs/cgroup")
    relative = next((line[3:] for line in Path("/proc/self/cgroup").read_text().splitlines() if line.startswith("0::")), None)
    require(relative is not None and (cgroup_root / "cgroup.controllers").exists(), "Visible cgroup v2 accounting is required; host-only capacity is insufficient")
    if relative is not None:
        candidate = cgroup_root / relative.lstrip("/")
        current = candidate if candidate.is_dir() else cgroup_root
        while current.is_relative_to(cgroup_root):
            maximum, used = current / "memory.max", current / "memory.current"
            if maximum.exists() and used.exists():
                limit = maximum.read_text().strip()
                if limit != "max":
                    available = min(available, max(0, int(limit) - int(used.read_text())))
            cpu_max = current / "cpu.max"
            if cpu_max.exists():
                quota, period = cpu_max.read_text().split()
                if quota != "max":
                    cpus = min(cpus, int(quota) / int(period))
            if current == cgroup_root:
                break
            current = current.parent
    return available // MIB, cpus


def properties_from_meminfo(text: str) -> dict[str, int]:
    return {key: int(value.split()[0]) for key, value in (line.split(":", 1) for line in text.splitlines() if ":" in line)}


def check_ports(profile: dict) -> None:
    with contextlib.ExitStack() as stack:
        for role, port in profile["ports"].items():
            kinds = (socket.SOCK_STREAM, socket.SOCK_DGRAM) if role == "bedrock" else (socket.SOCK_STREAM,)
            for kind in kinds:
                sock = stack.enter_context(socket.socket(socket.AF_INET, kind))
                try:
                    sock.bind(("127.0.0.1", port))
                except OSError as exc:
                    raise PilotError(f"Required {role} {'UDP' if kind == socket.SOCK_DGRAM else 'TCP'} port is unavailable") from exc


def check_headroom(root: Path, profile: dict) -> dict:
    memory, cpus = available_resources()
    need = sum(profile["heap_mib"].values()) + profile["native_budget_mib"] + profile["shared_reserve_mib"]
    require(memory >= need, f"Need at least {need} MiB available including native memory and shared headroom; found {memory} MiB")
    require(cpus >= profile["minimum_cpus"], "Insufficient CPU quota or affinity for the three-process pilot")
    require(os.getloadavg()[0] <= cpus, "Host load already exceeds the effective CPU budget; defer the pilot")
    require(shutil.disk_usage(root).free // MIB >= profile["minimum_disk_free_mib"], "Insufficient free disk headroom")
    require(disk_usage(root) <= profile["runtime_disk_budget_mib"], "Pilot runtime exceeds its cooperative disk budget")
    check_ports(profile)
    return {"available_memory_mib": memory, "required_memory_mib": need, "effective_cpus": cpus}


def varint(number: int) -> bytes:
    number &= 0xFFFFFFFF
    out = bytearray()
    while True:
        value = number & 0x7F
        number >>= 7
        out.append(value | (0x80 if number else 0))
        if not number:
            return bytes(out)


def receive_exact(sock, count: int) -> bytes:
    result = bytearray()
    while len(result) < count:
        block = sock.recv(count - len(result))
        require(bool(block), "Truncated server-list response")
        result.extend(block)
    return bytes(result)


def receive_varint(sock) -> int:
    value = 0
    for shift in range(0, 35, 7):
        byte = receive_exact(sock, 1)[0]
        value |= (byte & 0x7F) << shift
        if not byte & 0x80:
            return value
    raise PilotError("Oversized server-list VarInt")


def java_status(port: int) -> dict:
    with socket.create_connection(("127.0.0.1", port), timeout=2) as sock:
        sock.settimeout(2)
        hostname = b"127.0.0.1"
        handshake = b"\x00" + varint(-1) + varint(len(hostname)) + hostname + struct.pack(">H", port) + b"\x01"
        sock.sendall(varint(len(handshake)) + handshake + b"\x01\x00")
        length = receive_varint(sock)
        require(1 <= length <= MIB, "Invalid server-list packet size")
        packet = receive_exact(sock, length)
    class Reader:
        def __init__(self, value): self.stream = io.BytesIO(value)
        def recv(self, size): return self.stream.read(size)
    reader = Reader(packet)
    require(receive_varint(reader) == 0, "Not a server-list status response")
    size = receive_varint(reader)
    require(size <= MIB, "Oversized server-list JSON")
    data = json.loads(receive_exact(reader, size))
    require(isinstance(data, dict) and isinstance(data.get("version"), dict) and isinstance(data.get("players"), dict), "Malformed server-list JSON")
    return {"protocol": data["version"].get("protocol"), "online": data["players"].get("online")}


def probe(root: Path, profile: dict) -> dict:
    result = {"java_status": {}, "bedrock_signaling_tcp": False, "real_java_login": "not tested", "real_bedrock_login": "not tested"}
    for role in ("hub", "game", "proxy"):
        try:
            result["java_status"][role] = {"ok": True, **java_status(profile["ports"][role])}
        except (OSError, PilotError, ValueError):
            result["java_status"][role] = {"ok": False}
    try:
        with socket.create_connection(("127.0.0.1", profile["ports"]["bedrock"]), timeout=2):
            result["bedrock_signaling_tcp"] = True
    except OSError:
        pass
    result["bedrock_udp_gameplay"] = "requires an authenticated Bedrock client; TCP is insufficient"
    return result


def disk_usage(root: Path) -> int:
    total = 0
    for directory, dirs, files in os.walk(root, followlinks=False):
        for name in dirs + files:
            path = Path(directory) / name
            require(not path.is_symlink(), "Runtime symlink detected; stopping the pilot")
        for name in files:
            try:
                total += (Path(directory) / name).stat().st_size
            except FileNotFoundError:
                pass  # rotating logs
    return total // MIB


def stop_children(children: dict, timeout: int) -> bool:
    clean = True
    for role in ("proxy", "game", "hub"):
        process = children.get(role)
        if process is not None:
            try:
                if process.poll() is None:
                    process.stdin.write(b"shutdown\n" if role == "proxy" else b"stop\n")
                    process.stdin.flush()
            except (OSError, ValueError, subprocess.SubprocessError):
                clean = False
    deadline = time.monotonic() + timeout
    for process in children.values():
        try:
            process.wait(timeout=max(0.1, deadline - time.monotonic()))
        except (OSError, subprocess.SubprocessError):
            clean = False
            try:
                process.terminate()  # only a child handle from this supervisor
            except (OSError, subprocess.SubprocessError):
                pass  # continue cleanup for every other owned child
    for process in children.values():
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            clean = False
            try:
                process.kill()
                process.wait(timeout=10)
            except (OSError, subprocess.SubprocessError):
                pass
        except (OSError, subprocess.SubprocessError):
            clean = False
        finally:
            if process.stdin is not None:
                try:
                    process.stdin.close()
                except (OSError, ValueError):
                    clean = False
        clean = clean and process.returncode == 0
    return clean


def run(root: Path, profile: dict, lock: dict, java: str) -> None:
    with lease(root):
        validate_config(root, profile)
        require(eula_accepted(root), "Minecraft EULA is unaccepted. The owner must review it and manually set eula=true in both backend eula.txt files")
        require_transport_launchable(profile)
        require(os.geteuid() != 0, "Run the pilot as a dedicated unprivileged Linux user, never root")
        verify_all(root, lock)
        executable = java_executable(java)
        check_headroom(root, profile)
        session = secrets.token_hex(16)
        stop_requested = False
        children: dict[str, subprocess.Popen] = {}
        state = {"session": session, "state": "starting", "pid": os.getpid(), "ready": [], "heartbeat": time.time()}

        def handle_signal(signum, frame):
            nonlocal stop_requested
            stop_requested = True

        def requested():
            nonlocal stop_requested
            request = root / ".stop-request.json"
            if request.exists():
                require(read_json(request).get("session") == session, "Stale stop request; stopping this supervisor conservatively")
                stop_requested = True
            return stop_requested

        def publish():
            state["heartbeat"] = time.time()
            atomic_json(root / ".running.json", state)

        (root / ".stop-request.json").unlink(missing_ok=True)
        old_handlers = {sig: signal.signal(sig, handle_signal) for sig in (signal.SIGINT, signal.SIGTERM)}
        logs = []
        try:
            publish()
            for role in ("hub", "game", "proxy"):
                require(not requested(), "Stop requested during startup")
                directory = root / role
                log_path = safe_path(directory / "console.log")
                flags = os.O_WRONLY | os.O_APPEND | os.O_CREAT | getattr(os, "O_NOFOLLOW", 0)
                log = os.fdopen(os.open(log_path, flags, 0o600), "ab", buffering=0)
                logs.append(log)
                args = [executable, "-Xms256M", f"-Xmx{profile['heap_mib'][role]}M", "-XX:ActiveProcessorCount=2", "-Dfile.encoding=UTF-8", "-jar", "server.jar"]
                if role != "proxy":
                    args.append("--nogui")
                children[role] = subprocess.Popen(args, cwd=directory, stdin=subprocess.PIPE, stdout=log, stderr=subprocess.STDOUT,
                                                  env=clean_java_environment(), start_new_session=True)
                deadline = time.monotonic() + profile["startup_timeout_seconds"]
                while True:
                    require(not requested(), "Stop requested during startup")
                    require(all(child.poll() is None for child in children.values()), f"A JVM exited while starting {role}; inspect private console.log files")
                    require(time.monotonic() < deadline, f"Startup timed out for {role}; inspect its private console.log")
                    try:
                        java_status(profile["ports"][role])
                        if role == "proxy":
                            with socket.create_connection(("127.0.0.1", profile["ports"]["bedrock"]), timeout=2):
                                pass
                            if not safe_path(root / "proxy/plugins/floodgate/key.pem").is_file():
                                raise OSError("Floodgate has not generated its local key yet")
                        break
                    except (OSError, PilotError, ValueError):
                        publish()
                        time.sleep(0.5)
                state["ready"].append(role)
                publish()
                print(f"{role}: Java server-list status responds (login is not yet verified)", flush=True)
            state["state"] = "running"
            publish()
            print("Pilot running on loopback. Ctrl-C or the stop command requests a graceful shutdown.", flush=True)
            pressure_strikes = 0
            while not requested():
                require(all(child.poll() is None for child in children.values()), "A pilot JVM exited; stopping the remaining JVMs")
                memory, _ = available_resources()
                pressure_strikes = pressure_strikes + 1 if memory < profile["shared_reserve_mib"] else 0
                require(pressure_strikes < 3, "Shared memory headroom remained below reserve; stopping the pilot")
                require(shutil.disk_usage(root).free // MIB >= profile["stop_disk_free_mib"], "Disk free space fell below reserve; stopping the pilot")
                require(disk_usage(root) <= profile["runtime_disk_budget_mib"], "Pilot exceeded its cooperative disk budget; stopping it")
                publish()
                for _ in range(20):
                    if requested():
                        break
                    time.sleep(0.5)
        except (PilotError, OSError, ValueError) as exc:
            state["failure"] = str(exc)
            raise
        finally:
            already_failing = sys.exc_info()[0] is not None
            state["state"] = "stopping"
            try:
                publish()
            finally:
                clean = stop_children(children, profile["shutdown_timeout_seconds"])
                for log in logs:
                    log.close()
                for sig, handler in old_handlers.items():
                    signal.signal(sig, handler)
                state["state"] = "stopped" if clean else "unclean-stop"
                publish()
                (root / ".stop-request.json").unlink(missing_ok=True)
                print("Pilot stopped gracefully." if clean else "One or more JVMs did not stop cleanly; inspect logs and worlds before restarting.", flush=True)
            if not already_failing:
                require(clean, "Pilot shutdown was not clean")


def request_stop(root: Path) -> None:
    try:
        with lease(root):
            raise PilotError("No active pilot supervisor; this tool never signals a saved PID")
    except BusyError:
        state = read_json(root / ".running.json")
        require(state.get("state") in ("starting", "running", "stopping") and time.time() - state.get("heartbeat", 0) < 35, "Supervisor heartbeat is stale; inspect processes locally before recovery")
        atomic_json(root / ".stop-request.json", {"session": state["session"]})
        print("Graceful stop requested. Wait for the foreground supervisor to report completion.")


def allow_player(root: Path, name: str, identity: str) -> None:
    require(re.fullmatch(r"[A-Za-z0-9_.-]{1,32}", name) is not None, "Use the authenticated Java name or Floodgate-prefixed Bedrock name")
    try:
        identity = str(uuid.UUID(identity))
    except ValueError as exc:
        raise PilotError("Supply the player's verified authentication UUID") from exc
    with lease(root):
        for role in ("hub", "game"):
            path = root / role / "whitelist.json"
            entries = read_json(path)
            require(isinstance(entries, list) and len(entries) < 100, "Unexpected or oversized whitelist")
            require(all(isinstance(e, dict) and set(e) == {"uuid", "name"} for e in entries), "Unexpected whitelist format")
            entries = [e for e in entries if e["uuid"] != identity]
            entries.append({"uuid": identity, "name": name})
            atomic_json(path, entries)
    print("Verified identity added to both backend whitelists; no operator permission was granted.")


def live_runtime_processes(root: Path) -> list[int]:
    """Detect manually started or orphaned processes before a stopped snapshot."""
    active = []
    for directory in Path("/proc").iterdir():
        if not directory.name.isdigit() or int(directory.name) == os.getpid():
            continue
        try:
            if directory.stat().st_uid != os.getuid():
                continue
            location = Path(os.readlink(directory / "cwd").removesuffix(" (deleted)"))
            if location.is_relative_to(root):
                active.append(int(directory.name))
        except (FileNotFoundError, ProcessLookupError):
            continue
        except PermissionError as exc:
            raise PilotError("Cannot inspect another same-user process; a stopped snapshot cannot be established") from exc
    return active


@contextlib.contextmanager
def stopped_worlds(root: Path, profile: dict):
    import fcntl
    with lease(root), contextlib.ExitStack() as locks:
        state_path = root / ".running.json"
        if state_path.exists():
            require(read_json(state_path).get("state") == "stopped", "A clean stopped state is required before backup; restart/recover and stop cleanly first")
        require(not live_runtime_processes(root), "A process is still using the runtime directory; stop it before backup")
        check_ports(profile)
        for role in ("hub", "game"):
            for path in (root / role).rglob("session.lock"):
                safe_path(path)
                metadata = path.stat(follow_symlinks=False)
                require(stat.S_ISREG(metadata.st_mode) and metadata.st_nlink == 1, "World locks must be regular files without hard links")
                fd = os.open(path, os.O_RDWR | getattr(os, "O_NOFOLLOW", 0) | os.O_NONBLOCK)
                locks.callback(os.close, fd)
                metadata = os.fstat(fd)
                require(stat.S_ISREG(metadata.st_mode) and metadata.st_nlink == 1, "World locks must be regular files without hard links")
                try:
                    # Java's world FileLock uses POSIX record locks on Linux.
                    fcntl.lockf(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError as exc:
                    raise PilotError("A Minecraft world is still locked; no consistent backup was made") from exc
        yield


def snapshot_excluded(relative: str) -> bool:
    # Closing a second fd for an inode releases this process's classic POSIX
    # record locks on that inode. Never reopen session.lock while holding it.
    # Minecraft creates these transient lock files afresh after restoration.
    if Path(relative).name == "session.lock":
        return True
    if relative in {".supervisor.lock", ".running.json", ".stop-request.json"}:
        return True
    if relative in {path for paths in DESTINATIONS.values() for path in paths}:
        return True
    parts = Path(relative).parts
    return len(parts) >= 2 and parts[0] in {"hub", "game", "proxy"} and parts[1] in {"console.log", "logs", "cache", "libraries", "versions"}


def backup_stopped(root: Path, profile: dict, output: Path) -> None:
    output = safe_path(output)
    require(not output.is_relative_to(root) and not output.is_relative_to(REPO), "Backups must be outside the source runtime and repository")
    require(output.name.endswith(".tar.gz") and output.parent.is_dir(), "Choose a .tar.gz path in an existing private directory")
    sidecar = output.with_name(output.name + ".sha256.json")
    require(not output.exists() and not sidecar.exists(), "Refusing to overwrite an existing backup or checksum")
    temporary = output.with_name(output.name + "." + secrets.token_hex(8) + ".part")
    checksum_temporary = sidecar.with_name(sidecar.name + "." + secrets.token_hex(8) + ".part")
    installed = checksum_installed = False
    try:
        with stopped_worlds(root, profile):
            validate_config(root, profile)
            require(disk_usage(root) <= profile["runtime_disk_budget_mib"], "Source exceeds its configured disk budget")
            entries = {}
            manifest = {"schema": 1, "kind": "metallic-mc-stopped-backup", "created_unix": int(time.time()),
                        "consistency": "stopped with supervisor, process, port, and world-lock checks",
                        "excludes": "entrypoint jars, bootstrap caches/libraries/versions, runtime logs, supervisor state, session.lock files", "files": entries}
            flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
            with os.fdopen(os.open(temporary, flags, 0o600), "wb") as raw:
                with tarfile.open(fileobj=raw, mode="w:gz", compresslevel=6) as archive:
                    for path in sorted(root.rglob("*")):
                        safe_path(path)
                        if path.is_dir():
                            continue
                        relative = path.relative_to(root).as_posix()
                        require(relative != ".pilot-backup-manifest.json", "Reserved backup manifest name exists in the runtime")
                        if snapshot_excluded(relative):
                            continue
                        require(len(relative) <= SNAPSHOT_MAX_NAME and "\\" not in relative, "Backup source path cannot be restored safely")
                        metadata = path.stat(follow_symlinks=False)
                        require(stat.S_ISREG(metadata.st_mode) and metadata.st_nlink == 1, "Backup source files must be regular files without hard links")
                        require(len(entries) < SNAPSHOT_MAX_FILES, "Backup file count exceeds the pilot budget")
                        with os.fdopen(os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | os.O_NONBLOCK), "rb") as source:
                            before = os.fstat(source.fileno())
                            require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1, "Backup source files must be regular files without hard links")
                            digest = hashlib.sha256()

                            class DigestReader:
                                def read(self, size=-1):
                                    data = source.read(size)
                                    digest.update(data)
                                    return data

                            member = tarfile.TarInfo(relative)
                            member.size, member.mode, member.mtime = before.st_size, 0o600, int(before.st_mtime)
                            archive.addfile(member, DigestReader())
                            after = os.fstat(source.fileno())
                            require((before.st_ino, before.st_size, before.st_mtime_ns) == (after.st_ino, after.st_size, after.st_mtime_ns), "A file changed during the stopped snapshot; backup refused")
                            entries[relative] = {"sha256": digest.hexdigest(), "size": before.st_size}
                    payload = json.dumps(manifest, sort_keys=True).encode()
                    require(len(payload) <= SNAPSHOT_MAX_MANIFEST, "Backup manifest exceeds the restore budget")
                    member = tarfile.TarInfo(".pilot-backup-manifest.json")
                    member.size, member.mode = len(payload), 0o600
                    archive.addfile(member, io.BytesIO(payload))
                raw.flush()
                os.fsync(raw.fileno())
            with temporary.open("rb") as stream:
                digest = hashlib.file_digest(stream, "sha256").hexdigest()
            write_new(checksum_temporary, json.dumps({"schema": 1, "sha256": digest, "size": temporary.stat().st_size}, indent=2) + "\n")
            os.link(temporary, output)
            installed = True
            os.link(checksum_temporary, sidecar)
            checksum_installed = True
        print(f"Stopped backup created: {output.name}; {len(entries)} files. Archive and per-file SHA-256 verification are required on restore.")
    except BaseException:
        if checksum_installed:
            sidecar.unlink(missing_ok=True)
        if installed:
            output.unlink(missing_ok=True)
        raise
    finally:
        temporary.unlink(missing_ok=True)
        checksum_temporary.unlink(missing_ok=True)


def restore_new(archive_path: Path, root: Path) -> None:
    archive_path, root = safe_path(archive_path), safe_path(root)
    require(not root.exists() and not root.is_relative_to(REPO), "Restore requires a new directory outside the repository; existing worlds are never overwritten")
    sidecar = read_json(archive_path.with_name(archive_path.name + ".sha256.json"))
    require(sidecar.get("schema") == 1 and isinstance(sidecar.get("sha256"), str) and re.fullmatch(r"[0-9a-f]{64}", sidecar["sha256"]), "Invalid archive checksum file")
    require(type(sidecar.get("size")) is int and archive_path.stat().st_size == sidecar["size"] and 0 < sidecar["size"] <= 11 * 1024 * MIB, "Backup archive size mismatch or budget exceeded")
    created = False
    try:
        with archive_path.open("rb") as raw:
            require(secrets.compare_digest(hashlib.file_digest(raw, "sha256").hexdigest(), sidecar["sha256"]), "Backup archive checksum mismatch; nothing restored")
            raw.seek(0)
            root.mkdir(mode=0o700, parents=False)
            created = True
            with lease(root), tarfile.open(fileobj=raw, mode="r:gz") as archive:
                actual, manifest = {}, None
                total = 0
                for member in archive:
                    name = member.name
                    require(member.isfile() and not member.issym() and not member.islnk(), "Backup contains a non-regular entry")
                    require(name and len(name) <= SNAPSHOT_MAX_NAME and not name.startswith("/") and "\\" not in name
                            and all(part not in {"", ".", ".."} for part in name.split("/")), "Unsafe backup path")
                    require(0 <= member.size <= 10 * 1024 * MIB, "Backup entry size exceeded")
                    total += member.size
                    require(total <= 10 * 1024 * MIB, "Expanded backup exceeds the restore budget")
                    source = archive.extractfile(member)
                    require(source is not None, "Backup entry has no data")
                    if name == ".pilot-backup-manifest.json":
                        require(manifest is None and member.size <= SNAPSHOT_MAX_MANIFEST, "Invalid backup manifest")
                        manifest = json.loads(source.read(SNAPSHOT_MAX_MANIFEST + 1))
                        continue
                    require(len(actual) < SNAPSHOT_MAX_FILES, "Backup file count exceeded")
                    require(name not in actual and not snapshot_excluded(name), "Duplicate or excluded path in backup")
                    path = safe_path(root / name)
                    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
                    digest = hashlib.sha256()
                    count = 0
                    with os.fdopen(os.open(path, flags, 0o600), "wb") as destination:
                        while block := source.read(MIB):
                            count += len(block)
                            digest.update(block)
                            destination.write(block)
                    require(count == member.size, "Truncated backup entry")
                    actual[name] = {"size": count, "sha256": digest.hexdigest()}
                require(isinstance(manifest, dict) and manifest.get("schema") == 1 and manifest.get("kind") == "metallic-mc-stopped-backup"
                        and manifest.get("files") == actual, "Backup per-file manifest verification failed")
                _, profile, _ = runtime(root)
                validate_config(root, profile)
                require(disk_usage(root) <= profile["runtime_disk_budget_mib"], "Restored files exceed their configured disk budget")
        print("Backup restored to a new private directory; archive and every file digest verified. Run fetch, validate --artifacts, and preflight before any start.")
    except BaseException:
        if created:
            shutil.rmtree(root)  # only the new directory created by this operation
        raise


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    for command in ("init", "fetch", "validate", "preflight", "run", "stop", "status", "probe", "allow", "backup", "restore"):
        child = sub.add_parser(command)
        child.add_argument("--root", type=Path, required=True, help="private runtime directory outside the repository")
        if command == "init":
            child.add_argument("--profile", type=Path, default=BUNDLE / "profile.json")
            child.add_argument("--lock", type=Path, default=BUNDLE / "artifacts.lock.json")
        if command in ("run", "preflight"):
            child.add_argument("--java", default="java", help="installed Java 25 executable")
        if command == "validate":
            child.add_argument("--artifacts", action="store_true", help="also verify every downloaded JAR")
        if command == "allow":
            child.add_argument("--name", required=True)
            child.add_argument("--uuid", required=True)
        if command == "backup":
            child.add_argument("--output", type=Path, required=True, help="new private .tar.gz path outside the runtime")
        if command == "restore":
            child.add_argument("--archive", type=Path, required=True, help="archive with its .sha256.json sidecar")
    args = parser.parse_args(argv)
    if hasattr(os, "umask"):
        os.umask(0o077)
    try:
        if args.command == "init":
            initialize(args.root, args.profile, args.lock)
            return 0
        if args.command == "restore":
            restore_new(args.archive, args.root)
            return 0
        root, profile, lock = runtime(args.root)
        if args.command == "fetch":
            fetch(root, profile, lock)
        elif args.command == "validate":
            validate_config(root, profile)
            if args.artifacts:
                verify_all(root, lock)
            blockers = transport_launch_blockers(profile)
            print(json.dumps({"config": "valid", "artifacts": "verified" if args.artifacts else "not checked", "eula": "accepted by owner" if eula_accepted(root) else "not accepted", "launch_ready": not blockers, "launch_blockers": blockers, "java_login": "not tested", "bedrock_login": "not tested"}, indent=2))
        elif args.command == "preflight":
            with lease(root):
                validate_config(root, profile)
                require_transport_launchable(profile)
                verify_all(root, lock)
                java_executable(args.java)
                result = check_headroom(root, profile)
                result["eula_accepted"] = eula_accepted(root)
                print(json.dumps(result, indent=2))
        elif args.command == "run":
            run(root, profile, lock, args.java)
        elif args.command == "stop":
            request_stop(root)
        elif args.command == "probe":
            result = probe(root, profile)
            result["launch_blockers"] = transport_launch_blockers(profile)
            print(json.dumps(result, indent=2))
            return 0 if not result["launch_blockers"] and all(x["ok"] for x in result["java_status"].values()) and result["bedrock_signaling_tcp"] else 1
        elif args.command == "allow":
            allow_player(root, args.name, args.uuid)
        elif args.command == "backup":
            backup_stopped(root, profile, args.output)
        elif args.command == "status":
            path = root / ".running.json"
            state = read_json(path) if path.exists() else {"state": "never started"}
            state.pop("session", None)
            state.pop("pid", None)
            if "heartbeat" in state:
                state["heartbeat_age_seconds"] = round(time.time() - state.pop("heartbeat"), 1)
                if state["state"] in ("starting", "running") and state["heartbeat_age_seconds"] > 35:
                    state["state"] = "stale; inspect locally"
            print(json.dumps(state, indent=2))
        return 0
    except (PilotError, OSError, ValueError, subprocess.SubprocessError, tarfile.TarError) as exc:
        print(f"Pilot blocked: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
