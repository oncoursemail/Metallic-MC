"""No root installation or running systemd manager is used by these tests."""
import argparse
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('metallic_deploy', Path(__file__).with_name('deploy.py'))
deploy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deploy)


class PreparationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.control = self.base / 'control'
        self.agent = self.base / 'agent'
        self.control.write_bytes(b'fixture-control-not-an-executable')
        self.agent.write_bytes(b'fixture-agent-not-an-executable')
        self.args = argparse.Namespace(control=self.control, agent=self.agent,
            control_sha256=hashlib.sha256(self.control.read_bytes()).hexdigest(),
            agent_sha256=hashlib.sha256(self.agent.read_bytes()).hexdigest(),
            output=self.base / 'bundle', port=18080, node_id='test-node', architect=False)

    def prepare(self):
        output = io.StringIO()
        old_umask = os.umask(0o077)
        try:
            with contextlib.redirect_stdout(output):
                deploy.prepare(self.args)
        finally:
            os.umask(old_umask)
        return output.getvalue()

    def test_prepares_private_separated_secrets_and_runnable_modes(self):
        output = self.prepare()
        root = self.args.output
        self.assertEqual(stat.S_IMODE(root.stat().st_mode), 0o700)
        control = deploy.validate_env(root, 18080)
        agent = deploy.read_env(root / 'agent.env')
        self.assertEqual(control['METALLIC_ARCHITECT_ENABLED'], 'false')
        self.assertNotIn('METALLIC_ADMIN_PASSWORD', agent)
        self.assertNotIn(control['METALLIC_AGENT_TOKEN'], output)
        self.assertNotIn(control['METALLIC_ADMIN_PASSWORD'], output)
        for name in ('controller.env', 'agent.env'):
            self.assertEqual(stat.S_IMODE((root / name).stat().st_mode), 0o600)
        for name in ('metallic-control', 'metallic-agent'):
            self.assertEqual(stat.S_IMODE((root / name).stat().st_mode), 0o755)
        for name in deploy.NAMES:
            self.assertEqual(stat.S_IMODE((root / name).stat().st_mode), 0o644)

    def test_wrong_digest_and_existing_output_are_refused(self):
        self.args.control_sha256 = '0' * 64
        with self.assertRaisesRegex(ValueError, 'checksum'):
            self.prepare()
        self.assertFalse(self.args.output.exists())
        self.args.control_sha256 = hashlib.sha256(self.control.read_bytes()).hexdigest()
        self.prepare()
        before = (self.args.output / 'controller.env').read_bytes()
        with self.assertRaisesRegex(ValueError, 'never overwritten'):
            self.prepare()
        self.assertEqual((self.args.output / 'controller.env').read_bytes(), before)

    def test_public_endpoint_and_symlink_input_are_refused(self):
        self.prepare()
        path = self.args.output / 'agent.env'
        path.write_text(path.read_text().replace('127.0.0.1', '0.0.0.0'))
        with self.assertRaisesRegex(ValueError, 'loopback'):
            deploy.validate_env(self.args.output, 18080)
        link = self.base / 'link'
        link.symlink_to(self.control)
        with self.assertRaisesRegex(ValueError, 'Symlink'):
            deploy.digest(link)

    def test_explicit_architect_uses_dedicated_durable_directory(self):
        self.args.architect = True
        self.prepare()
        control = deploy.validate_env(self.args.output, 18080)
        self.assertEqual(control['METALLIC_HOSTING_STORE_PATH'], '/var/lib/metallic-mc/controller/hosting/hosts.json')
        self.assertEqual(control['METALLIC_ARCHITECT_ENABLED'], 'true')

    def test_environment_edited_after_bundle_validation_is_rejected_after_copy(self):
        self.prepare()
        deploy.validate_env(self.args.output, 18080)
        # Simulate the bundle owner editing the file after install's preflight.
        source = self.args.output / 'agent.env'
        source.write_text(source.read_text().replace('127.0.0.1', '192.0.2.10'))
        destination = self.base / 'installed-config'
        destination.mkdir(mode=0o700)
        with self.assertRaisesRegex(ValueError, 'same pinned loopback'):
            deploy.install_environment(self.args.output, destination, 18080)
        self.assertTrue((destination / 'agent.env').is_file())
        self.assertEqual(stat.S_IMODE((destination / 'agent.env').stat().st_mode), 0o600)

    def test_units_have_separate_identities_and_bounded_shutdown(self):
        control = (deploy.HERE / (deploy.NAMES[0] + '.in')).read_text()
        agent = (deploy.HERE / (deploy.NAMES[1] + '.in')).read_text()
        self.assertIn('User=metallic-control\n', control)
        self.assertIn('User=metallic-metrics\n', agent)
        self.assertNotIn('ReadWritePaths=', agent)
        self.assertIn('TimeoutStopSec=30s', control)
        for unit in (control, agent):
            self.assertIn('Restart=on-failure', unit)
            self.assertIn('StartLimitBurst=5', unit)
            self.assertIn('KillMode=control-group', unit)
            self.assertIn('NoNewPrivileges=yes', unit)


if __name__ == '__main__':
    unittest.main()
