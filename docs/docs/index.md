# Pi‑Hole Operator

!!! warning "Early‑Stage Project"
  The Pi‑Hole Operator is still evolving. Expect frequent breaking changes and incomplete documentation.  
  If you encounter problems or have ideas, please open an issue or pull request on GitHub.

---

## 📚 Table of Contents
1. [What We’re Building](#what-we-re-building)
2. [Key Features](#key-features)
3. [Quick Start](#quick-start)
4. [Architecture Overview](#architecture-overview)
5. [Getting Started – Deploy a Cluster](#getting-started‑deploy-a-cluster)
6. [Contributing](#contributing)
7. [Documentation Links](#documentation-links)

---

## 🚀 What We’re Building
The Pi‑Hole Operator brings Kubernetes‑native management to a self‑hosted ad‑blocking DNS solution:

| Target | Why it matters |
|--------|----------------|
| **Declarative Management** | One YAML file defines the entire Pi‑Hole cluster; the operator provisions, upgrades, and backs up automatically. |
| **Scalable DNS** | Run Pi‑Hole as a replicated deployment or StatefulSet, automatically load‑balancing queries. |
| **Automated Configuration** | Keep whitelist/blacklist, upstream servers, and custom settings in sync across all replicas. |
| **Health & Observability** | Prometheus metrics, readiness/liveness probes, and integration with Kubernetes monitoring tools. |
| **Rolling Updates & Rollbacks** | Zero‑downtime upgrades backed by a robust restore strategy. |

---

## ✨ Key Features
- **CRD (`PiHoleCluster`)** – Exposes all Pi‑Hole configuration options as declarative fields.  
- **StatefulSet Deployment** – Persistent storage for logs, hosts files, and configuration.  
- **Dynamic Upstream Management** – Auto‑updates upstream DNS servers based on cluster topology or external secrets.  
- **Metrics & Alerts** – Prometheus exporter for query counts, blocked domains, and latency; alerts for high failure rates.  
- **Ingress & LoadBalancer Support** – Optional Ingress with cert‑manager TLS or public IP via `LoadBalancer`.  
- **Helm‑OCI Chart** – Easy installation from `ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator`.  

---

## ⚡ Quick Start
```bash
# Install the OCI Helm chart (requires Helm 3.10+)
helm install pi-hole-operator \
  --namespace pi-hole-operator-system \
  --create-namespace \
  ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator:latest
```

After the chart is deployed, create a `PiHoleCluster`:

```yaml
apiVersion: v1alpha1.example.com/v1
kind: PiHoleCluster
metadata:
  name: demo
spec:
  replicas: 3          # read‑only replicas
  ingress:
    enabled: true
    domain: "pi.example.com"
  service:
    type: LoadBalancer   # optional – expose via public IP
```

```bash
kubectl apply -f demo.yaml
```

The operator will provision the deployment, service, ingress, and metrics automatically.

---

## 🏗️ Architecture Overview
```
+-----------------+          +---------------------+
| PiHoleCluster  | CRD      | Operator (manager) |
+-----------------+ <------> +---------------------+
          ^                     |
          |                     v
   +-----------+        +---------------+
   | Deployment|        | Service/Ingress|
   +-----------+        +---------------+
          |
          v
     Pi‑Hole Pods (Read‑write + Replicas)
```

* **Controller** watches `PiHoleCluster` resources and reconciles the desired state.
* **Deployment** hosts one read‑write pod + N replicas (configurable).
* **Service** exposes the cluster internally; `LoadBalancer` or Ingress can be used for external access.
* **Metrics** are exposed on `/metrics` and can be scraped by Prometheus.

---

## 👩‍💻 Contributing
1. Fork the repo and create a feature branch (`git checkout -b feat/your-feature`).
2. Run tests before committing:
   ```bash
   make test          # unit tests
   make test-e2e      # e2E tests (requires kind/minikube)
   ```
3. Follow the Go style (`go fmt`), keep changes focused, and add tests for new logic.
4. Open a PR – we’ll review and merge if it meets the guidelines.

---

## 📖 Documentation Links
| Topic | Link |
|-------|------|
| Installation & Upgrade | <https://supporterino.de/pi-hole-operator/docs/installation> |
| Usage & CRD Reference | <https://supporterino.de/pi-hole-operator/docs/usage> |
| Architecture & Design | <https://supporterino.de/pi-hole-operator/docs/architecture> |
| Troubleshooting | <https://supporterino.de/pi-hole-operator/docs/troubleshooting> |
| FAQ | <https://supporterino.de/pi-hole-operator/docs/faq> |

---

> **Note**: The documentation site is hosted at <https://supporterino.de/pi-hole-operator/>.  Browse the sections above for detailed guidance.
