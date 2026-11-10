#!/usr/bin/env python3
"""Verify local node readiness, DNS, and PVC persistence without deploying apps."""

import json
from pathlib import Path
import subprocess
import time
from urllib.parse import urlparse
import uuid


def main():
    repo = Path(__file__).resolve().parents[2]
    config = repo / "infra/terraform/environments/local/kubeconfig.json"
    document = json.loads(config.read_text())
    for cluster in document["clusters"]:
        endpoint = urlparse(cluster["cluster"]["server"])
        if endpoint.scheme != "https" or endpoint.hostname not in ("localhost", "127.0.0.1"):
            raise SystemExit("Refusing to test a cluster outside local Floci.")
    base = ["kubectl", f"--kubeconfig={config}", "--request-timeout=30s"]

    # The emulator can be healthy before its restored Kubernetes API is ready.
    print("Waiting up to 180 seconds for the local Kubernetes API...", flush=True)
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        try:
            ready = subprocess.run(
                base + ["get", "--raw=/readyz", "--request-timeout=5s"],
                text=True, capture_output=True,
                timeout=min(10, max(0.1, deadline - time.monotonic())),
            )
            if ready.returncode == 0 and ready.stdout.strip() == "ok":
                break
        except subprocess.TimeoutExpired:
            pass
        time.sleep(min(2, max(0, deadline - time.monotonic())))
    else:
        raise SystemExit("Kubernetes API did not become ready; inspect the Floci child container logs.")

    def run(*args, data=None):
        subprocess.run(base + list(args), input=data, text=True, check=True, timeout=240)

    def apply(resource):
        run("apply", "-f", "-", data=json.dumps(resource))

    namespace = f"floci-validation-{uuid.uuid4().hex[:8]}"
    marker = uuid.uuid4().hex
    run("wait", "--for=condition=Ready", "node", "--all", "--timeout=180s")
    run("get", "nodes", "-o", "wide")
    run("get", "storageclass", "local-path")
    run("create", "namespace", namespace)
    try:
        apply({
            "apiVersion": "v1", "kind": "PersistentVolumeClaim",
            "metadata": {"name": "data", "namespace": namespace},
            "spec": {"accessModes": ["ReadWriteOnce"],
                     "resources": {"requests": {"storage": "32Mi"}}},
        })

        def pod(name, command):
            return {
                "apiVersion": "v1", "kind": "Pod",
                "metadata": {"name": name, "namespace": namespace},
                "spec": {
                    "restartPolicy": "Never",
                    "terminationGracePeriodSeconds": 1,
                    "containers": [{
                        "name": "check", "image": "busybox:1.37.0",
                        "command": ["sh", "-ec", command],
                        "resources": {"requests": {"cpu": "10m", "memory": "16Mi"},
                                      "limits": {"cpu": "100m", "memory": "32Mi"}},
                        "volumeMounts": [{"name": "data", "mountPath": "/data"}],
                    }],
                    "volumes": [{"name": "data", "persistentVolumeClaim": {"claimName": "data"}}],
                },
            }

        apply(pod("writer", f"printf '%s' '{marker}' > /data/marker; sleep 600"))
        run("-n", namespace, "wait", "--for=condition=Ready", "pod/writer", "--timeout=180s")
        run("-n", namespace, "wait", "--for=jsonpath={.status.phase}=Bound", "pvc/data", "--timeout=30s")
        run("-n", namespace, "exec", "writer", "--", "nslookup", "kubernetes.default.svc.cluster.local")
        run("-n", namespace, "delete", "pod", "writer", "--wait=true", "--timeout=60s")
        apply(pod("reader", f"test \"$(cat /data/marker)\" = '{marker}'; sleep 600"))
        run("-n", namespace, "wait", "--for=condition=Ready", "pod/reader", "--timeout=180s")
        # Check explicitly: a brief Ready state alone is not evidence of the marker.
        run("-n", namespace, "exec", "reader", "--", "sh", "-ec",
            f"test \"$(cat /data/marker)\" = '{marker}'")
        print("PASS: node Ready, DNS resolves, default PVC binds, data survives pod replacement.")
    except Exception:
        run("-n", namespace, "get", "pods,pvc,events")
        raise
    finally:
        run("delete", "namespace", namespace, "--wait=true", "--timeout=180s")


if __name__ == "__main__":
    main()
