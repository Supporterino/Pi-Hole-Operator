# Architecture

The Pi‑Hole Operator follows the classic Kubernetes operator pattern.  
Below is a high‑level diagram followed by a component‑by‑component walk‑through.

```
+-----------------+          +---------------------+
| PiHoleCluster  | CRD      | Operator (manager) |
+-----------------+ <------> +---------------------+
          ^                            |
          |                            v
   +-----------+                +---------------+
   | Deployment|  (read‑write + replicas) |
   +-----------+                +---------------+
          |                            |
          v                            v
     Pi‑Hole Pods (dnsmasq)      Service / Ingress
          |                            |
          +-----------+    +------------+
                      |    |  Metrics   |
                      +---->+ (Prometheus)|
                           +------------+
```

## Components

| Component | Responsibility |
|-----------|----------------|
| **CRD (`PiHoleCluster`)** | Declarative description of the desired Pi‑Hole cluster (replicas, ingress, config). |
| **Controller Manager** | Watches `PiHoleCluster` resources and reconciles the Deployment, Service, Ingress, ConfigMap, Secrets. |
| **Deployment** | Runs one read‑write Pi‑Hole pod and N read‑only replicas. Uses a shared PVC for persistence (optional). |
| **Service** | Exposes the Pi‑Hole DNS port (`53/udp`) internally. Can be a `LoadBalancer` if required. |
| **Ingress** | Optional HTTPS ingress that terminates TLS via cert‑manager and forwards to the service. |
| **Metrics** | Exporter container (sidecar or embedded) exposing `/metrics` for Prometheus. |
| **Webhook** | Mutating/Validating admission webhook that ensures CR validity and injects default values. TLS is provided by cert‑manager. |

## Data Flow

1. **User creates** a `PiHoleCluster` CR.
2. The **operator** receives the event, validates it via the webhook, and starts reconciliation.
3. It creates/updates the **Deployment** (read‑write + replicas) and a **Service**.
4. If `ingress.enabled`, the operator creates an **Ingress** and requests a TLS cert.
5. The operator watches the Pi‑Hole pods’ API, syncing configuration and ad‑lists from the specified sources.
6. Metrics are exposed on `/metrics` and can be scraped by Prometheus.

## Persistence

The operator uses a `PersistentVolumeClaim` for the Pi‑Hole data directory (`/etc/pihole`).  
You can customize storage size via `spec.persistence.size` in the CR.

---

**Next**

- [Usage](../usage.md) – How to create a `PiHoleCluster`.
- [Troubleshooting](../troubleshooting.md) – Common problems and fixes.