# Pi‑Hole Operator
**Manage high‑available Pi‑Hole clusters on Kubernetes**

---

## 📖 Overview

The Pi‑Hole Operator is a declarative, Kubernetes‑native operator that provisions and maintains one or more Pi‑Hole instances with built‑in HA, Ingress support, and optional LoadBalancer exposure.  
It is designed for **DevOps Engineers**, **Platform Engineers**, and anyone building or maintaining Kubernetes operators.

---

## 🚀 Features

| Feature | Description |
|---------|-------------|
| **Declarative management** | A single `PiHoleCluster` CRD creates the whole stack (deployment, service, ingress). |
| **High‑availability** | One read‑write pod + configurable number of read‑only replicas. |
| **Ingress support** | Optional Ingress with custom domain and TLS via cert‑manager. |
| **LoadBalancer exposure** | When you need a public IP, simply set `spec.service.type: LoadBalancer`. |
| **Metrics & health probes** | Exporter on `/metrics`, liveness/readiness checks. |
| **Helm‑OCI chart** | Pull the chart directly from `ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator`. |
| **CRD‑only install** | Use the `install.yaml` in each release for a lightweight deployment. |

---

## 📦 Prerequisites

| Component | Minimum version |
|-----------|-----------------|
| **Kubernetes** | 1.28+ (tested on GKE, EKS, Minikube) |
| **Helm v3** | ≥ 3.10 (OCI support required) |
| **kubectl** | 1.28+ |
| **Go (build)** | 1.22+ |

> If you want to expose Pi‑Hole via a LoadBalancer, ensure your cluster supports the `LoadBalancer` service type (e.g., GKE, EKS).

---

## 📥 Installation

### 1️⃣ Helm‑OCI Chart (recommended)

```bash
# Install the chart from the repo
helm install pi-hole-operator \
  --namespace pi-hole-operator-system \
  --create-namespace \
  ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator:latest
```

The chart installs:

* CRDs (`PiHoleCluster`)
* RBAC and controller manager
* Optional components (Ingress, metrics, cert‑manager) – enable via Helm values

### 2️⃣ CRD‑Only Install (lightweight)

If you prefer to install only the CRDs and apply a custom deployment:

```bash
kubectl apply -f https://github.com/supporterino/pi-hole-operator/releases/latest/download/install.yaml
```

This file contains the CRDs and a minimal `PiHoleCluster` example you can edit.

---

## 🛠️ Using the Operator

Below is a minimal `PiHoleCluster` example.  
Add your own values (e.g., domain, replica count) before applying.

```yaml
apiVersion: v1alpha1.example.com/v1
kind: PiHoleCluster
metadata:
  name: example
spec:
  replicas: 3          # number of read‑only replicas
  ingress:
    enabled: true
    domain: "pi.example.com"
  service:
    type: LoadBalancer   # optional – expose via public IP
```

Apply the CRD:

```bash
kubectl apply -f piholecluster.yaml
```

The operator will create:

* A Deployment with one read‑write pod and the specified replicas
* A Service (`ClusterIP` by default, `LoadBalancer` if set)
* An Ingress (if enabled) with TLS via cert‑manager
* Metrics and health probes

---

## 📚 Documentation

For a deeper dive into configuration, advanced usage, and troubleshooting, visit the full docs:

[https://supporterino.de/pi-hole-operator/](https://supporterino.de/pi-hole-operator/)

---

## 🤝 Contributing

1. **Fork** the repository and create a feature branch: `git checkout -b feat/your-feature`.
2. **Run tests** before committing:

   ```bash
   make lint``        # lint code
   make test          # unit tests
   make build         # ensure build is working
   ```

3. **Follow the style** (`go fmt`, keep changes focused, add tests for new logic).
4. **Open a PR** – we’ll review and merge if it meets the guidelines.

---

## 📄 License

Apache 2.0 – see [LICENSE](LICENSE).

---