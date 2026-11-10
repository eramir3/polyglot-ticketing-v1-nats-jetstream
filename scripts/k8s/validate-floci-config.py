#!/usr/bin/env python3
"""Validate the isolated Floci config without displaying its credentials."""
import json
from pathlib import Path
import sys
from urllib.parse import urlparse


def main():
    path, name, endpoint = sys.argv[1:]
    try:
        config = json.loads(Path(path).read_text())
        context = next(c['context'] for c in config['contexts'] if c['name'] == name)
        cluster = next(c['cluster'] for c in config['clusters'] if c['name'] == context['cluster'])
        url = urlparse(cluster['server'])
        if (config['current-context'] != name or context['cluster'] != name
                or cluster['server'] != endpoint or url.scheme != 'https'
                or url.hostname not in ('localhost', '127.0.0.1')
                or cluster.get('insecure-skip-tls-verify', False)
                or not cluster.get('certificate-authority-data')):
            raise ValueError()
    except (OSError, ValueError, KeyError, StopIteration, TypeError):
        raise SystemExit('Error: isolated Floci kubeconfig does not match the local Terraform cluster. Run make floci-eks-kubeconfig.')


if __name__ == '__main__':
    main()
