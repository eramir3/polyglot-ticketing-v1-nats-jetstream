#!/usr/bin/env python3
"""Check all five local Secret files before creating any Kubernetes resources."""
import base64
import json
from pathlib import Path
import subprocess
import sys
from urllib.parse import unquote, urlparse

SERVICES = ('identity', 'tickets', 'orders', 'payments', 'expiration')


def validate(directory, namespace, kubectl_command):
    if directory.is_symlink() or not directory.is_dir():
        raise ValueError('Secret directory must be a regular directory.')
    for service in SERVICES:
        path = directory / f'{service}-secret.yaml'
        if path.is_symlink() or not path.is_file():
            raise ValueError(f'Missing regular Secret file for {service}.')
        # Parse through kubectl to avoid adding a YAML dependency. Suppress error
        # details because malformed manifests can contain credentials.
        result = subprocess.run(kubectl_command + ['--namespace', namespace, 'create', '--dry-run=client', '--validate=false',
                                 '-f', str(path), '-o', 'json'], capture_output=True, text=True)
        if result.returncode:
            raise ValueError(f'Could not parse Secret file for {service}.')
        try:
            document = json.loads(result.stdout)
            metadata = document['metadata']
            if (document['kind'] != 'Secret' or document['apiVersion'] != 'v1'
                    or document.get('type', 'Opaque') != 'Opaque'
                    or metadata['name'] != f'{service}-secret'
                    or metadata.get('namespace', namespace) != namespace):
                raise ValueError()
            values = {k: base64.b64decode(v, validate=True).decode()
                      for k, v in document.get('data', {}).items()}
            values.update(document.get('stringData', {}))
            required = ['REDIS_URL'] if service == 'expiration' else ['DATABASE_URL', 'POSTGRES_PASSWORD']
            if service == 'identity':
                required.append('BETTER_AUTH_SECRET')
            if any(not isinstance(values.get(k), str) or not values[k].strip()
                   or 'REPLACE_WITH' in values[k] for k in required):
                raise ValueError()
            if service != 'expiration':
                url = urlparse(values['DATABASE_URL'])
                if (url.scheme not in ('postgres', 'postgresql') or url.hostname != f'{service}-db'
                        or unquote(url.username or '') != service or url.port != 5432
                        or unquote(url.path) != f'/{service}-db'
                        or unquote(url.password or '') != values['POSTGRES_PASSWORD']):
                    raise ValueError()
            else:
                url = urlparse(values['REDIS_URL'])
                if url.scheme not in ('redis', 'rediss') or url.hostname != 'redis' or url.port != 6379:
                    raise ValueError()
            if service == 'identity' and len(values['BETTER_AUTH_SECRET']) < 32:
                raise ValueError()
        except (ValueError, KeyError, TypeError, UnicodeError):
            raise ValueError(f'Invalid or unpopulated Secret for {service}; check its name, namespace, required keys and internal connection settings.') from None


if __name__ == '__main__':
    try:
        validate(Path(sys.argv[1]), sys.argv[2], sys.argv[3:])
    except ValueError as error:
        raise SystemExit(f'Error: {error}')
    print('All five Secret files validated; values were not displayed.')
