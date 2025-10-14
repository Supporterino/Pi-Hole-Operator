# Pi‑Hole Operator

!!! warning "Early‑Stage Project"  
    The Pi‑Hole Operator is currently in its infancy.  
      
      - Frequent breaking changes are expected as we iterate on the API and the controller logic.  
      - Documentation, tests, and examples are still being fleshed out.  
      - If you run into issues or have ideas, please open an issue or pull request on GitHub – community feedback is crucial.

---

## What We’re Building

The Pi‑Hole Operator brings the power of Kubernetes to a self‑hosted ad‑blocking DNS solution.  
Its core mission is to **simplify the deployment, scaling, and lifecycle management of Pi‑Hole** in a cloud‑native environment.

| Target | Why it matters |
|--------|----------------|
| **Declarative Management** | Define a Pi‑Hole cluster with a single YAML file; the operator takes care of provisioning, upgrades, and backups. |
| **Scalable DNS** | Run Pi‑Hole in a replicated deployment or as a stateful set, automatically load‑balancing queries across nodes. |
| **Automated Configuration** | Keep your whitelist/blacklist, upstream servers, and custom settings in sync across all replicas. |
| **Health & Observability** | Expose Prometheus metrics, readiness/liveness probes, and integrate with Kubernetes’ native monitoring tools. |
| **Rolling Updates & Rollbacks** | Seamless version upgrades with zero‑downtime, backed by a robust strategy for restoring previous states. |

---

## Key Features (in‑progress)

- **Custom Resource Definition (CRD)** – `PiHole` resource exposes all Pi‑Hole configuration options as declarative fields.
- **StatefulSet Deployment** – ensures persistent storage for DNS logs, hosts files, and configuration.
- **Dynamic Upstream Management** – automatically updates upstream DNS servers based on cluster topology or external secrets.
- **Metrics & Alerts** – Prometheus exporter for query counts, blocked domains, and latency; alerts for high failure rates.

---
