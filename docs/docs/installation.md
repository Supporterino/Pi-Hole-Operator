# Installation

The Pi‑Hole Operator is distributed as an OCI Helm chart.  
Below are the steps for a fresh install, plus optional values you can tweak.

## Prerequisites

| Component | Minimum version |
|-----------|-----------------|
| **Helm 3.10+** (OCI support) | |
| **Kubernetes 1.28+** | |
| **cert‑manager (optional)** | ≥ 1.12 |

> If you plan to use the operator’s Ingress or LoadBalancer features, make sure your cluster supports those resources (e.g., GKE/EKS).

## 1️⃣ Add the OCI registry

```bash
# Log in to GitHub Container Registry (once per shell session)
helm registry login ghcr.io

# Optional: add a Helm repo for convenience
helm repo add pi-hole-operator https://ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator
```

> The chart is available under the OCI path  
> `ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator`.

## 2️⃣ Install the chart

```bash
# Pull the latest image (optional, useful for offline installs)
helm pull oci://ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator --version latest

# Install the chart directly from OCI
helm install pi-hole-operator \
  oci://ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator \
  --namespace pi-hole-operator-system \
  --create-namespace
```

> The chart installs the CRDs, RBAC, controller manager and all optional components.

## 3️⃣ Optional values

You can override any value by passing a custom `values.yaml` or using the `--set` flag.

| Value | Description | Default |
|-------|-------------|---------|
| `metrics.enabled` | Enable Prometheus metrics exporter | `true` |
| `ingress.enabled` | Create an Ingress for the Pi‑Hole service | `false` |
| `service.type` | Service type (`ClusterIP`, `LoadBalancer`) | `ClusterIP` |
| `certmanager.enable` | Enable cert‑manager webhook TLS | `true` |

> **Fetching the default values**  
> The project ships a helper called `mcp`.  Run:

```bash
mcp get-values pi-hole-operator
```

> to print the chart’s default `values.yaml` on your terminal.

## 4️⃣ Verify

```bash
kubectl get pods -n pi-hole-operator-system
kubectl describe deployment pi-hole-operator-controller-manager -n pi-hole-operator-system
```

If the operator pod is running and `READY` shows `1/1`, you’re good to go!

---

**Next steps**

- [Usage](../usage.md) – Create your first `PiHoleCluster`.
- [Architecture](../architecture.md) – Understand how the operator works.