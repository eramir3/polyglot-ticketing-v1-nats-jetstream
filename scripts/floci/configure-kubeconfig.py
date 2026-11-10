#!/usr/bin/env python3
"""Write an isolated kubeconfig from the local Terraform outputs."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
from urllib.parse import urlparse


def main():
    repo = Path(__file__).resolve().parents[2]
    root = repo / "infra/terraform/environments/local"
    outputs = json.loads(subprocess.check_output(
        ["terraform", f"-chdir={root}", "output", "-json"], text=True
    ))
    values = {name: output["value"] for name, output in outputs.items()}
    endpoint = urlparse(values["cluster_endpoint"])
    if endpoint.scheme != "https" or endpoint.hostname not in ("localhost", "127.0.0.1"):
        raise SystemExit("Refusing a cluster endpoint outside local Floci.")

    name = values["cluster_name"]
    credentials = values["operator_credentials"]
    config = {
        "apiVersion": "v1",
        "kind": "Config",
        "clusters": [{"name": name, "cluster": {
            "server": values["cluster_endpoint"],
            "certificate-authority-data": values["cluster_certificate_authority_data"],
        }}],
        "contexts": [{"name": name, "context": {"cluster": name, "user": name}}],
        "current-context": name,
        "users": [{"name": name, "user": {"exec": {
            "apiVersion": "client.authentication.k8s.io/v1beta1",
            "command": "env",
            "args": ["-u", "AWS_PROFILE", "-u", "AWS_DEFAULT_PROFILE", "aws",
                     "--endpoint-url", "http://127.0.0.1:4566", "--region",
                     values["region"], "eks", "get-token", "--cluster-name", name],
            "env": [{"name": key, "value": value} for key, value in {
                "AWS_ACCESS_KEY_ID": credentials["access_key_id"],
                "AWS_SECRET_ACCESS_KEY": credentials["secret_access_key"],
                "AWS_SESSION_TOKEN": "",
                "AWS_CONFIG_FILE": "/dev/null",
                "AWS_SHARED_CREDENTIALS_FILE": "/dev/null",
                "AWS_EC2_METADATA_DISABLED": "true",
            }.items()],
        }}}],
    }

    # mkstemp starts at 0600; replace atomically without exposing credentials.
    target = root / "kubeconfig.json"
    fd, temporary = tempfile.mkstemp(prefix=".kubeconfig-", dir=root)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(config, stream, indent=2)
            stream.write("\n")
        os.replace(temporary, target)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
    print(f"Wrote {target} (0600); default kubeconfig was not changed.")


if __name__ == "__main__":
    main()
