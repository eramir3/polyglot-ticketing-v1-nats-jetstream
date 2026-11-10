"""Executable workflow contracts; all external tools are replaced with recording stubs."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[3]
STUB = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ['CALL_LOG'], 'a') as stream:
    stream.write(json.dumps([name, *args]) + '\n')
if name == 'kubectl':
    if args == ['config', 'current-context']: print(os.environ.get('TEST_CONTEXT', 'minikube'))
    elif '--raw=/readyz' in args: print('ok')
    elif any('status.nodeInfo.architecture' in a for a in args): print('amd64')
    elif '--dry-run=client' in args and '-f' in args:
        # Tests supply JSON manifests, a valid YAML subset.
        print(pathlib.Path(args[args.index('-f') + 1]).read_text())
elif name == 'terraform':
    print('https://localhost:6500' if args[-1] == 'cluster_endpoint' else 'polyglot-ticketing-local')
elif name == 'docker':
    if args[0] == 'ps': print(os.environ.get('TEST_CONTAINERS', 'container123'))
    elif args[:2] == ['image', 'inspect']: print('amd64')
    elif args[0] == 'save':
        if os.environ.get('FAIL_SAVE'): sys.exit(9)
        sys.stdout.write('image archive')
    elif args[0] == 'build' and os.environ.get('FAIL_BUILD') in args: sys.exit(7)
    elif args[0] == 'exec' and 'import' in args:
        sys.stdin.read()
        if os.environ.get('FAIL_IMPORT'): sys.exit(8)
    elif args[0] == 'exec' and 'list' in args:
        for service in ('tickets','identity','orders','payments','expiration','api-gateway'):
            print('docker.io/library/' + service + ':dev')
'''


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        for name in ('kubectl', 'minikube', 'docker', 'make', 'pnpm', 'terraform'):
            path = self.bin / name
            path.write_text(STUB)
            path.chmod(0o755)
        # Mirror only scripts and a synthetic kubeconfig. Never read real credentials.
        self.repo = self.root / 'repo'
        (self.repo / 'scripts').mkdir(parents=True)
        (self.repo / 'scripts' / 'k8s').symlink_to(REPO / 'scripts/k8s')
        # common.sh resolves repo relative to its source spelling, so the mirror
        # can use symlinks without resolving the original repository's state.
        config_root = self.repo / 'infra/terraform/environments/local'
        config_root.mkdir(parents=True)
        (self.repo / 'infra/k8s').symlink_to(REPO / 'infra/k8s')
        self.config = config_root / 'kubeconfig.json'
        self.document = {
            'current-context': 'polyglot-ticketing-local',
            'contexts': [{'name': 'polyglot-ticketing-local', 'context': {'cluster': 'polyglot-ticketing-local'}}],
            'clusters': [{'name': 'polyglot-ticketing-local', 'cluster': {
                'server': 'https://localhost:6500', 'certificate-authority-data': 'dummy'}}],
        }
        self.config.write_text(json.dumps(self.document))
        self.log = self.root / 'calls.jsonl'
        self.env = {**os.environ, 'PATH': f'{self.bin}:{os.environ["PATH"]}',
                    'CALL_LOG': str(self.log), 'K8S_SECRETS_DIR': str(self.root / 'secrets')}
        self.env.pop('K8S_TARGET', None)
        self.env.pop('MINIKUBE_PROFILE', None)
        self.env.pop('K8S_NAMESPACE', None)

    def run_script(self, script, **env):
        return subprocess.run(['bash', str(self.repo / 'scripts/k8s' / script)],
                              env={**self.env, **env}, capture_output=True, text=True)

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def test_default_minikube_builds_every_image_before_loading(self):
        result = self.run_script('prepare-images.sh')
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls()
        builds = [i for i, c in enumerate(calls) if c[:2] == ['docker', 'build']]
        loads = [i for i, c in enumerate(calls) if c[0] == 'minikube' and 'load' in c]
        self.assertEqual(len(builds), 6)
        self.assertEqual(len(loads), 6)
        self.assertLess(max(builds), min(loads))
        self.assertIn(['minikube', '-p', 'minikube', 'image', 'load', '--daemon', '--overwrite=true', 'tickets:dev'], calls)

    def test_custom_profile_and_wrong_context(self):
        self.assertEqual(self.run_script('list-secrets.sh', MINIKUBE_PROFILE='lab', TEST_CONTEXT='lab').returncode, 0)
        self.assertIn(['kubectl', '--context', 'lab', 'get', 'secrets', '-n', 'polyglot-ticketing'], self.calls())
        self.log.unlink()
        self.assertNotEqual(self.run_script('deploy-apps.sh', TEST_CONTEXT='production').returncode, 0)
        self.assertEqual(self.calls(), [['kubectl', 'config', 'current-context']])

    def test_unknown_target_does_not_contact_tools(self):
        self.assertNotEqual(self.run_script('deploy-dependencies.sh', K8S_TARGET='aws').returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_floci_uses_explicit_config_and_containerd_namespace(self):
        result = self.run_script('prepare-images.sh', K8S_TARGET='floci', TEST_CONTEXT='production')
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls()
        self.assertFalse(any(c[0] == 'minikube' for c in calls))
        self.assertTrue(all('--kubeconfig' in c and '--context' in c for c in calls if c[0] == 'kubectl'))
        imports = [c for c in calls if c[0] == 'docker' and 'import' in c]
        self.assertEqual(len(imports), 6)
        self.assertIn(['docker', 'exec', '-i', 'container123', 'ctr', '--namespace', 'k8s.io', 'images', 'import', '--platform', 'linux/amd64', '-'], imports)

    def test_floci_deployment_keeps_all_rollout_waits(self):
        result = self.run_script('deploy-apps.sh', K8S_TARGET='floci')
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = [c for c in self.calls() if c[0] == 'kubectl']
        self.assertTrue(all('--kubeconfig' in c for c in calls))
        self.assertEqual(sum('rollout' in c for c in calls), 13)
        self.assertEqual(sum('apply' in c for c in calls), 6)

    def test_rejects_nonlocal_or_mismatched_cluster_before_contact(self):
        for endpoint in ('https://real-eks.example.com', 'https://localhost:9999'):
            self.document['clusters'][0]['cluster']['server'] = endpoint
            self.config.write_text(json.dumps(self.document))
            if self.log.exists(): self.log.unlink()
            result = self.run_script('deploy-apps.sh', K8S_TARGET='floci')
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(any(c[0] == 'kubectl' for c in self.calls()))

    def test_ambiguous_container_stops_before_build(self):
        result = self.run_script('prepare-images.sh', K8S_TARGET='floci', TEST_CONTAINERS='one\ntwo')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(c[:2] == ['docker', 'build'] for c in self.calls()))

    def test_build_save_and_import_failures_propagate(self):
        for failure in ({'FAIL_BUILD': 'tickets:dev'}, {'FAIL_SAVE': '1'}, {'FAIL_IMPORT': '1'}):
            if self.log.exists(): self.log.unlink()
            result = self.run_script('prepare-images.sh', K8S_TARGET='floci', **failure)
            self.assertNotEqual(result.returncode, 0)
            if 'FAIL_BUILD' in failure:
                self.assertFalse(any(c[:2] == ['docker', 'save'] for c in self.calls()))

    def test_gateway_commands_reject_floci(self):
        for script in ('deploy-gateway.sh', 'port-forward-gateway.sh'):
            self.assertNotEqual(self.run_script(script, K8S_TARGET='floci').returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_secret_generation_preserves_existing_files_and_creates_only_five(self):
        result = self.run_script('prepare-secrets.sh', K8S_TARGET='floci')
        self.assertEqual(result.returncode, 0, result.stderr)
        directory = Path(self.env['K8S_SECRETS_DIR'])
        before = {p.name: p.read_bytes() for p in directory.iterdir()}
        self.assertEqual(len(before), 5)
        self.assertFalse(any(b'REPLACE_WITH' in content for content in before.values()))
        self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
        self.assertTrue(all(p.stat().st_mode & 0o777 == 0o600 for p in directory.iterdir()))
        self.assertEqual(self.run_script('prepare-secrets.sh', K8S_TARGET='floci').returncode, 0)
        self.assertEqual(before, {p.name: p.read_bytes() for p in directory.iterdir()})
        self.assertEqual(self.calls(), [])

    def test_default_secret_directories_are_separate(self):
        for target, expected in (
                ('minikube', '/tmp/k8s-polyglot-ticketing-secrets'),
                ('floci', '/tmp/k8s-polyglot-ticketing-floci-secrets')):
            result = subprocess.run(
                ['bash', '-c', 'source "$1"; printf "%s" "$destination"', 'test',
                 str(self.repo / 'scripts/k8s/common.sh')],
                env={**self.env, 'K8S_TARGET': target, 'K8S_SECRETS_DIR': ''},
                capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, expected)

    def test_direct_forward_uses_floci_config_and_application_namespace(self):
        result = self.run_script('port-forward-api.sh', K8S_TARGET='floci', K8S_NAMESPACE='demo')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls()[-1][-7:], [
            '-n', 'demo', 'port-forward', '--address', 'localhost', 'service/api-gateway', '3000:3000'])
        self.assertIn('--kubeconfig', self.calls()[-1])

    def test_secrets_validated_as_a_group_before_mutation(self):
        directory = Path(self.env['K8S_SECRETS_DIR'])
        directory.mkdir()
        for service in ('identity', 'tickets', 'orders', 'payments', 'expiration'):
            values = {'REDIS_URL': 'redis://redis:6379'} if service == 'expiration' else {
                'DATABASE_URL': f'postgresql://{service}:password@{service}-db:5432/{service}-db',
                'POSTGRES_PASSWORD': 'password'}
            if service == 'identity': values['BETTER_AUTH_SECRET'] = 'a' * 64
            (directory / f'{service}-secret.yaml').write_text(json.dumps({
                'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': f'{service}-secret'},
                'stringData': values}))
        # Last file invalid: none of the preceding four may be applied.
        last = directory / 'expiration-secret.yaml'
        good = last.read_text()
        last.write_text(good.replace('redis://redis:6379', 'REPLACE_WITH_URL'))
        result = self.run_script('apply-secrets.sh')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any('apply' in c for c in self.calls()))
        last.write_text(good)
        self.log.unlink()
        result = self.run_script('apply-secrets.sh')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sum('apply' in c for c in self.calls()), 6)
        self.assertNotIn('password', result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
