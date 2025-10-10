# Pi‑Hole Operator

**A Kubernetes operator that manages one or many Pi‑Hole clusters – fully declarative, secure and observability‑ready.**

> *Author:* supporterino  
> *License:* Apache 2.0

---

## Table of contents
1. [What is Pi‑Hole Operator?](#what-is-pi-hole-operator)
2. [Features](#features)
3. [Architecture](#architecture)
4. [Prerequisites](#prerequisites)
5. [Installation](#installation)
    * Helm chart
    * Installing the CRDs
6. [Using the Operator](#using-the-operator)
    * Creating a PiHoleCluster
    * Syncing lists & configuration
    * Ingress, monitoring and metrics
7. [Configuration](#configuration)
    * `values.yaml` knobs
    * Secret handling for the API password
8. [Local Development](#local-development)
9. [Testing](#testing)
10. [Contributing](#contributing)
11. [License](#license)

---

## What is Pi‑Hole Operator?

Pi‑Hole is a network‑wide ad blocker that runs as a DNS server.  
The **Pi‑Hole Operator** is a Kubernetes operator written with Kubebuilder that:

* Deploys and manages **Pi‑Hole clusters** (one read‑write pod + one or more read‑only replicas)
* Keeps the Pi‑Hole configuration and ad‑lists in sync with a Git repository or any other source
* Exposes the cluster via an Ingress (optional)
* Provides Prometheus metrics, ServiceMonitors and pod‑monitors
* Supports cert‑manager for webhook TLS
* Is fully declarative – you only describe the desired state in a `PiHoleCluster` CR

---

## Features

| Feature | Description |
|---------|-------------|
| **Declarative cluster** | One `PiHoleCluster` CR creates a read‑write pod + N replicas |
| **Sync** | Operator can sync configuration files and ad‑lists from a Git repo (or any URL) |
| **Ingress** | Optional Ingress with domain support |
| **Monitoring** | Exporter + ServiceMonitor / PodMonitor for Prometheus |
| **Webhooks & cert‑manager** | Validating and mutating admission webhooks with TLS |
| **NetworkPolicies** | Optional network‑policy for isolation |
| **Helm chart** | Install via Helm (`dist/chart`) with all CRDs, RBAC and optional components |
| **Local dev** | `local_deploy.sh` builds & pushes the image, then deploys with Helm |
| **e2E tests** | `test/e2e` contains integration tests (see the test folder) |
| **Observability** | Metrics, health probes and liveness/readiness checks |

---

## Architecture

```
+-----------------+          +--------------------+
| PiHoleCluster   |  CRD      |  Operator (manager)|
+-----------------+ <-------- +--------------------+
          ^                            |
          |                            v
   Spec & Status  <--->  Reconciler logic
          |                            |
          +----------------------------+
               +-----+------+-------+
               |  Pi‑Hole  |  Exporter|
               |   Pods    | (metrics)|
               +-----+------+-------+
```

* The operator runs in the `pi-hole-operator-system` namespace.
* Each `PiHoleCluster` creates:
    * **Read‑write** pod – the primary Pi‑Hole instance
    * **Read‑only replicas** (configurable via `spec.replicas`)
* The operator watches the CR and reconciles:
    * Deployment, Service, Ingress, ConfigMap
    * Syncing of configuration and ad‑lists via the Pi‑Hole API
* Optional components (metrics, webhooks, cert‑manager, network policy) are enabled via Helm values.

---

## Prerequisites

| Component | Minimum version |
|-----------|-----------------|
| Go | 1.22+ (used for building) |
| kubectl | 1.28+ |
| Helm v3 | ≥3.10 |
| Docker / Podman | ≥20.10 |
| Kubernetes cluster | 1.28+ (tested on GKE, EKS, Minikube) |
| cert‑manager | 1.12+ (if `certmanager.enable` is true) |

---

## Installation

### 1. Install the Helm chart

```bash
# Add the repo (if you host it elsewhere)
helm repo add pi-hole-operator https://example.com/charts
helm repo update

# Install (default values)
helm install pi-hole-operator pi-hole-operator/pi-hole-operator \
  --namespace pi-hole-operator-system \
  --create-namespace
```

The chart installs:

* CRDs (`PiHoleCluster`)
* RBAC
* Controller manager deployment
* Optional components per `values.yaml`

### 2. Install CRDs manually (optional)

If you prefer to install the CRDs separately:

```bash
kubectl apply -f config/crd/bases/
```

---

## Using the Operator

### Create a PiHoleCluster

```yaml
apiVersion: v1alpha1.example.com/v1
kind: PiHoleCluster
metadata:
  name: example
spec:
  replicas: 3          # read‑only replicas
  ingress:
    enabled: true
    domain: "pi.example.com"
  sync:
    config: true
    adLists: true
  monitoring:
    exporter:
      enabled: true
  config:
    apiPassword:
      secretRef:
        name: pihole-secret
        key: password
    env:
      TZ: "Europe/Berlin"
  persistence:
    size: 10Gi
```

Apply:

```bash
kubectl apply -f piholecluster.yaml
```

The operator will create the deployment, service, ingress and sync configuration automatically.

### Syncing lists & configuration

* The operator polls the Pi‑Hole API every 5 minutes (configurable via env var `SYNC_INTERVAL`).
* Add URLs under `spec.config.adLists`.  
  Example: `https://some.com/ads.txt`.

### Ingress, Monitoring & Metrics

* If `spec.ingress.enabled` is true, an Ingress will be created.
* Exporter metrics are exposed on `/metrics`.  
  Enable via `spec.monitoring.exporter.enabled`.
* Prometheus ServiceMonitor is created if `metrics.enable` in Helm values is true.

---

## Configuration

### Helm `values.yaml`

| Section | Key | Default |
|---------|-----|---------|
| `controllerManager` | `replicas` | 1 |
| `rbac.enable` | `true` | - |
| `crd.enable`, `crd.keep` | `true` | - |
| `metrics.enable` | `true` | - |
| `webhook.enable` | `true` | - |
| `prometheus.enable` | `false` | - |
| `certmanager.enable` | `true` | - |
| `networkPolicy.enable` | `false` | - |

### API password

The Pi‑Hole web interface requires a password.  
You can store it in a Kubernetes Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: pihole-secret
type: Opaque
data:
  password: <base64-encoded-password>
```

Then reference it in the CR:

```yaml
config:
  apiPassword:
    secretRef:
      name: pihole-secret
      key: password
```

---

## Local Development

The repository ships with a convenient script that builds, pushes and deploys the operator.

```bash
# Build & push image
./local_deploy.sh patch   # or major/minor/patch

# Deploy to your cluster (helm will use the image from the script)
```

The script:

1. Bumps the version in `.version`.
2. Builds the Docker image (`make docker-build`).
3. Pushes it to `registry.supporterino.de/supporterino/pihole-operator`.
4. Deploys the chart with Helm.

---

## Testing

* **Unit tests** – run `go test ./...`.
* **e2E tests** – located in `test/e2e`.  
  They create a temporary cluster (kind or minikube), deploy the operator and validate CR creation.

```bash
make test-e2e
```

---

## Contributing

1. Fork the repo.
2. Create a feature branch (`git checkout -b feat/…`).
3. Run `make test` to ensure all tests pass.
4. Submit a PR.

**Style guidelines**

* Follow the existing Go formatting (`go fmt`).
* Keep changes minimal – do not refactor unrelated code.
* Add unit tests for any new logic.

---

## License

Apache 2.0 – see [LICENSE](LICENSE).

---