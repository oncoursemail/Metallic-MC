#!/usr/bin/env python3
"""Prepare a private bundle, then explicitly install/rollback as root on Linux.

No command downloads files, accepts EULAs, manages Minecraft or touches Wings.
Preparation is unprivileged. Install never replaces existing secrets or state.
"""
from __future__ import annotations
import argparse
import contextlib
import grp
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shutil
import socket
import stat
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
PREFIX = Path('/opt/metallic-mc')
CONFIG = Path('/etc/metallic-mc')
STATE = Path('/var/lib/metallic-mc')
UNITS = Path('/etc/systemd/system')
NAMES = ('metallic-mc-control.service', 'metallic-mc-agent.service')
ACCOUNTS = ('metallic-control', 'metallic-metrics')
MARKER = 'Metallic-MC systemd bundle schema 1'


def need(ok, message):
    if not ok:
        raise ValueError(message)


def real(path):
    path = Path(os.path.abspath(path))
    need(all(not p.is_symlink() for p in (path, *path.parents)), 'Symlink path refused')
    return path


def digest(path):
    path = real(path)
    info = path.stat()
    need(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'Expected an unlinked regular input file')
    need(0 < info.st_size <= 128 * 1024 * 1024, 'Input is missing or oversized')
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def create(path, content, mode=0o600):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode), 'wb') as stream:
        os.fchmod(stream.fileno(), mode)
        stream.write(content.encode() if isinstance(content, str) else content)
        stream.flush()
        os.fsync(stream.fileno())


def checked_json(path):
    need(real(path).stat().st_size < 65536, 'Manifest too large')
    return json.loads(path.read_text())


def read_env(path):
    info = real(path).stat()
    need(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o600,
         'Environment files must be private regular files with mode 0600')
    need(info.st_size < 16384, 'Environment file too large')
    values = {}
    for line in path.read_text().splitlines():
        if not line or line.startswith('#'):
            continue
        key, separator, value = line.partition('=')
        need(separator and re.fullmatch(r'[A-Z_]+', key) and key not in values,
             'Environment file must use unique unquoted KEY=value lines')
        need(not any(x in value for x in ('"', "'", '\\', '\x00')), 'Unsupported environment quoting')
        values[key] = value
    return values


def validate_env(directory, port):
    control, agent = read_env(directory / 'controller.env'), read_env(directory / 'agent.env')
    need(control.get('METALLIC_LISTEN_ADDR') == f'127.0.0.1:{port}', 'Controller must use the pinned loopback address')
    need(agent.get('METALLIC_CONTROL_URL') == f'http://127.0.0.1:{port}', 'Agent must use the same pinned loopback address')
    need(not agent.get('METALLIC_AGENT_ALLOW_HTTP') and not agent.get('METALLIC_SERVICES'), 'Remote transport and service probes require a separate reviewed deployment')
    need(control.get('METALLIC_AGENT_TOKEN') == agent.get('METALLIC_AGENT_TOKEN') and len(agent.get('METALLIC_AGENT_TOKEN', '')) >= 24, 'Controller and agent tokens must match')
    need(len(control.get('METALLIC_ADMIN_PASSWORD', '')) >= 24, 'Administrator password must contain at least 24 characters')
    need(control.get('METALLIC_ARCHITECT_ENABLED') in ('true', 'false'), 'Architect must be explicitly true or false')
    store = control.get('METALLIC_HOSTING_STORE_PATH', '')
    need(store == (str(STATE / 'controller/hosting/hosts.json') if control['METALLIC_ARCHITECT_ENABLED'] == 'true' else ''), 'Unexpected durable store path')
    return control


def install_environment(source, destination, port):
    # Installation calls this only after establishing a root-owned 0700
    # destination. Validate the installed bytes, not the mutable source bundle.
    for name in ('controller.env', 'agent.env'):
        target = destination / name
        if not target.exists():
            create(target, (source / name).read_bytes())
        info = real(target).stat()
        need(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o600,
             'Existing environment files must remain installer-owned 0600')
    validate_env(destination, port)


def prepare(args):
    need(1024 <= args.port <= 65535, 'Use an unprivileged port')
    need(re.fullmatch(r'[A-Za-z0-9_.-]{1,64}', args.node_id), 'Invalid node ID')
    hashes = {}
    for role in ('control', 'agent'):
        pin = getattr(args, role + '_sha256')
        need(re.fullmatch(r'[0-9a-f]{64}', pin), 'Provide each reviewed SHA-256 pin')
        need(digest(getattr(args, role)) == pin, f'{role} input checksum mismatch')
        hashes['metallic-' + role] = pin
    root = real(args.output)
    need(not root.exists(), 'Output must be a new directory; existing bundles are never overwritten')
    root.mkdir(mode=0o700)
    for role in ('control', 'agent'):
        target = root / ('metallic-' + role)
        create(target, real(getattr(args, role)).read_bytes(), 0o755)
        need(digest(target) == hashes[target.name], 'Input changed during copy')
    for name in NAMES:
        content = (HERE / (name + '.in')).read_bytes()
        create(root / name, content, 0o644)
        hashes[name] = digest(root / name)
    token = secrets.token_hex(32)
    control = {
        'METALLIC_ADMIN_USER': 'admin', 'METALLIC_ADMIN_PASSWORD': secrets.token_hex(24),
        'METALLIC_AGENT_TOKEN': token, 'METALLIC_LISTEN_ADDR': f'127.0.0.1:{args.port}',
        'METALLIC_COOKIE_SECURE': 'false', 'METALLIC_ARCHITECT_ENABLED': str(args.architect).lower(),
        'METALLIC_HOSTING_STORE_PATH': str(STATE / 'controller/hosting/hosts.json') if args.architect else '',
    }
    agent = {'METALLIC_AGENT_TOKEN': token, 'METALLIC_CONTROL_URL': f'http://127.0.0.1:{args.port}',
             'METALLIC_NODE_ID': args.node_id, 'METALLIC_AGENT_DISK_PATH': '/', 'METALLIC_SERVICES': ''}
    for name, values in (('controller.env', control), ('agent.env', agent)):
        create(root / name, ''.join(f'{key}={value}\n' for key, value in values.items()))
    release = hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest()[:24]
    create(root / 'manifest.json', json.dumps({'schema': 1, 'release': release, 'port': args.port, 'hashes': hashes}, indent=2) + '\n')
    validate_env(root, args.port)
    print(f'Prepared release {release}. Secrets remain in private environment files. Nothing installed or started.')


def command(*args):
    return subprocess.run(args, check=True, text=True, capture_output=True)


def host_preflight():
    need(os.geteuid() == 0, 'Installation and rollback require an explicit root invocation')
    need(Path('/run/systemd/system').is_dir(), 'A running system systemd manager is required')
    need(Path('/sys/fs/cgroup/cgroup.controllers').is_file(), 'Unified cgroup v2 is required for these resource budgets')
    need(not Path('/etc/pterodactyl').exists(), 'Legacy Pterodactyl/Wings host refused; choose the dedicated control host')
    need(subprocess.run(['systemctl', 'is-active', '--quiet', 'wings.service']).returncode != 0,
         'Active Wings host refused')
    for path in (PREFIX, CONFIG, STATE, UNITS):
        real(path)
    version = int(command('systemctl', '--version').stdout.split()[1])
    need(version >= 249, 'systemd 249 or newer is required')


def own_directory(path, mode, uid=0, gid=0):
    real(path)
    if path.exists():
        info = path.stat()
        need(path.is_dir() and info.st_uid == uid and info.st_gid == gid and stat.S_IMODE(info.st_mode) == mode,
             f'Existing directory ownership or mode differs: {path}')
    else:
        path.mkdir(mode=mode)
        os.chmod(path, mode)
        os.chown(path, uid, gid)


@contextlib.contextmanager
def deployment_lock():
    need(os.geteuid() == 0, 'Installation and rollback require an explicit root invocation')
    import fcntl
    fd = os.open('/run/lock/metallic-mc-deploy.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        info = os.fstat(fd)
        need(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1, 'Unsafe deployment lock')
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        os.close(fd)


def switch_link(name, target):
    need(re.fullmatch(r'releases/[0-9a-f]{24}', target), 'Invalid release pointer')
    temporary = PREFIX / ('.' + name + '.' + secrets.token_hex(8))
    temporary.symlink_to(target)
    os.replace(temporary, PREFIX / name)
    sync_directory(PREFIX)


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def pointer(name):
    link = PREFIX / name
    if not link.is_symlink():
        need(not link.exists(), 'Expected a managed release symlink')
        return None
    target = os.readlink(link)
    need(re.fullmatch(r'releases/[0-9a-f]{24}', target), 'Unexpected release link target')
    return target


def install(args):
    host_preflight()
    root = real(args.bundle)
    info = root.stat()
    need(root.is_dir() and stat.S_IMODE(info.st_mode) == 0o700, 'Bundle directory must be private (0700)')
    manifest = checked_json(root / 'manifest.json')
    need(manifest.get('schema') == 1 and re.fullmatch(r'[0-9a-f]{24}', manifest.get('release', '')), 'Invalid manifest')
    need(set(manifest.get('hashes', {})) == {'metallic-control', 'metallic-agent', *NAMES}, 'Invalid release file set')
    for name, pin in manifest['hashes'].items():
        need(digest(root / name) == pin, f'Bundle checksum mismatch: {name}')
    for name in NAMES:
        need((root / name).read_bytes() == (HERE / (name + '.in')).read_bytes(), 'Bundle service differs from reviewed template')
    port = manifest['port']
    need(type(port) is int and 1024 <= port <= 65535, 'Invalid manifest port')
    validate_env(root, port)
    managed = CONFIG / 'deployment.json'
    previous = None
    if managed.exists():
        old = checked_json(managed)
        need(old.get('schema') == 1 and old.get('port') == port, 'Existing managed deployment uses another schema or port')
        validate_env(CONFIG, port)
        previous = pointer('current')
        need(previous is not None, 'Existing managed deployment has no release pointer')
    else:
        need(not any(path.exists() for path in (PREFIX, CONFIG, STATE)), 'Existing deployment directories refused; preserve them and investigate')
    for name in NAMES:
        loaded = command('systemctl', 'show', name, '--property=FragmentPath', '--value').stdout.strip()
        overrides = command('systemctl', 'show', name, '--property=DropInPaths', '--value').stdout.strip()
        target = UNITS / name
        need(not overrides and not (UNITS / (name + '.d')).exists(), 'Existing service overrides need separate review')
        if loaded or target.exists():
            need(managed.exists() and loaded == str(target) and digest(target) == manifest['hashes'][name],
                 'Existing or modified service unit refused')
    for account in ACCOUNTS:
        try:
            entry = pwd.getpwnam(account)
        except KeyError:
            entry = None
        if entry:
            need(managed.exists() and entry.pw_uid != 0 and entry.pw_dir == '/nonexistent' and entry.pw_shell.endswith('/nologin'), 'Existing unrelated service account refused')
            need(grp.getgrgid(entry.pw_gid).gr_name == account and set(os.getgrouplist(account, entry.pw_gid)) == {entry.pw_gid},
                 'Service identity has an unexpected primary or supplementary group')
        else:
            try:
                grp.getgrnam(account)
            except KeyError:
                pass
            else:
                raise ValueError('Existing unrelated service group refused')
    # Stop only this managed deployment; all unknown port owners remain untouched.
    if previous:
        command('systemctl', 'stop', *reversed(NAMES))
    with socket.socket() as test:
        try:
            test.bind(('127.0.0.1', port))
        except OSError as exc:
            raise ValueError('Controller port is occupied; no process was killed') from exc
    for account in ACCOUNTS:
        try:
            pwd.getpwnam(account)
        except KeyError:
            command('useradd', '--system', '--user-group', '--no-create-home', '--home-dir', '/nonexistent', '--shell', '/usr/sbin/nologin', account)
    own_directory(PREFIX, 0o755)
    own_directory(PREFIX / 'releases', 0o755)
    own_directory(CONFIG, 0o700)
    own_directory(STATE, 0o755)
    owner = pwd.getpwnam(ACCOUNTS[0])
    own_directory(STATE / 'controller', 0o700, owner.pw_uid, owner.pw_gid)
    own_directory(STATE / 'controller/hosting', 0o700, owner.pw_uid, owner.pw_gid)
    install_environment(root, CONFIG, port)
    release = PREFIX / 'releases' / manifest['release']
    if not release.exists():
        with tempfile.TemporaryDirectory(prefix='.staging-', dir=PREFIX / 'releases') as staging:
            staged = Path(staging)
            for name in ('metallic-control', 'metallic-agent'):
                create(staged / name, (root / name).read_bytes(), 0o755)
                need(digest(staged / name) == manifest['hashes'][name], 'Binary changed during release copy')
            create(staged / 'manifest.json', json.dumps(manifest) + '\n', 0o644)
            os.chmod(staged, 0o755)
            sync_directory(staged)
            os.rename(staged, release)
            sync_directory(release.parent)
    for name in ('metallic-control', 'metallic-agent'):
        need(digest(release / name) == manifest['hashes'][name], 'Stored release checksum mismatch')
    for name in NAMES:
        if not (UNITS / name).exists():
            create(UNITS / name, (root / name).read_bytes(), 0o644)
            need(digest(UNITS / name) == manifest['hashes'][name], 'Unit changed during copy; services were not started')
    if previous and previous != 'releases/' + manifest['release']:
        switch_link('previous', previous)
    switch_link('current', 'releases/' + manifest['release'])
    if not managed.exists():
        create(managed, json.dumps({'schema': 1, 'port': port}) + '\n')
    sync_directory(CONFIG)
    sync_directory(UNITS)
    command('systemctl', 'daemon-reload')
    if args.enable_and_start:
        command('systemctl', 'enable', *NAMES)
        command('systemctl', 'start', *NAMES)
        print('System services enabled and start requested. Verify health, telemetry and journal before declaring deployment healthy.')
    else:
        print('Installed; services were not enabled or started. Existing secrets and state were preserved.')


def rollback(args):
    host_preflight()
    need((CONFIG / 'deployment.json').exists(), 'No managed deployment')
    current, previous = pointer('current'), pointer('previous')
    need(current and previous and previous != current, 'No distinct previous release retained')
    manifest = checked_json(PREFIX / previous / 'manifest.json')
    deployment = checked_json(CONFIG / 'deployment.json')
    need(deployment.get('schema') == 1, 'Unknown deployment schema')
    validate_env(CONFIG, deployment['port'])
    for name in NAMES:
        fragment = command('systemctl', 'show', name, '--property=FragmentPath', '--value').stdout.strip()
        overrides = command('systemctl', 'show', name, '--property=DropInPaths', '--value').stdout.strip()
        need(fragment == str(UNITS / name) and not overrides and digest(UNITS / name) == manifest['hashes'][name],
             'Existing service policy differs from rollback release; review manually')
    for name in ('metallic-control', 'metallic-agent'):
        need(digest(PREFIX / previous / name) == manifest['hashes'][name], 'Rollback binary checksum mismatch')
    command('systemctl', 'stop', *reversed(NAMES))
    switch_link('current', previous)
    switch_link('previous', current)
    if args.start:
        command('systemctl', 'start', *NAMES)
    print('Previous binaries selected. Environment and durable state were not rolled back. Verify schema compatibility and service health.')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='action', required=True)
    p = commands.add_parser('prepare')
    for role in ('control', 'agent'):
        p.add_argument('--' + role, type=Path, required=True)
        p.add_argument('--' + role + '-sha256', required=True)
    p.add_argument('--output', type=Path, required=True)
    p.add_argument('--port', type=int, default=8080)
    p.add_argument('--node-id', default='metallic-control-host')
    p.add_argument('--architect', action='store_true', help='explicitly enable private Architect on FIRST install')
    p = commands.add_parser('install')
    p.add_argument('--bundle', type=Path, required=True)
    p.add_argument('--enable-and-start', action='store_true')
    p = commands.add_parser('rollback')
    p.add_argument('--start', action='store_true')
    args = parser.parse_args()
    try:
        if args.action == 'prepare':
            prepare(args)
        else:
            with deployment_lock():
                {'install': install, 'rollback': rollback}[args.action](args)
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        print(f'Deployment blocked: {type(exc).__name__}: {exc}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
