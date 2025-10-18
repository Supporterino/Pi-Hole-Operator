# Usage

Once the operator is running, you create a **PiHoleCluster** custom resource that describes your desired Pi‑Hole deployment.

## 1️⃣ Create a secret for the API password

```bash
kubectl create secret generic pihole-secret \
  --namespace pi-hole-operator-system \
  --from-literal=password='YOUR_PIHOLE_PASSWORD'
```

> The password is used by the operator to authenticate against the Pi‑Hole API.

## 2️⃣ Define a `PiHoleCluster`

```yaml
apiVersion: v1alpha1.example.com/v1
kind: PiHoleCluster
metadata:
  name: demo-cluster
spec:
  replicas: 3                # number of read‑only replicas
  ingress:
    enabled: true
    domain: "pi.example.com"
  service:
    type: LoadBalancer        # optional – expose via public IP
  config:
    apiPassword:
      secretRef:
        name: pihole-secret
        key: password
    env:
      TZ: "Europe/Berlin"
  sync:
    config: true
    adLists: true
```

> Replace `pi.example.com` with your real domain and adjust the environment variables as needed.

## 3️⃣ Apply the CR

```bash
kubectl apply -f demo-cluster.yaml
```

The operator will:

1. Create a Deployment with one read‑write pod + 3 replicas.
2. Expose the service (`ClusterIP` by default, `LoadBalancer` if set).
3. Create an Ingress (if enabled) and request a TLS cert via cert‑manager.
4. Sync Pi‑Hole configuration and ad‑lists.

## 4️⃣ Verify the deployment

```bash
# List pods
kubectl get pods -n pi-hole-operator-system

# View operator logs (for troubleshooting)
kubectl logs deployment/pi-hole-operator-controller-manager -n pi-hole-operator-system

# Check Pi‑Hole pod logs
kubectl logs deployment/demo-cluster -n pi-hole-operator-system

# Test DNS resolution (replace <service-ip> with the actual IP)
dig @<service-ip> example.com

# View metrics (port‑forward if needed)
kubectl port-forward svc/demo-cluster-metrics 9100 -n pi-hole-operator-system
curl http://localhost:9100/metrics
```

## 5️⃣ Common operations

| Operation | Command |
|-----------|---------|
| Scale replicas | `kubectl patch piholecluster/demo-cluster -p '{"spec":{"replicas":5}}'` |
| Update ad‑lists | Edit `spec.config.adLists` in the CR and reapply |
| Rolling upgrade of Pi‑Hole image | Update the `image` field in the Deployment or use a Helm upgrade with new chart version |

---

**Next**

- [Architecture](../architecture.md) – Dive into how the operator reconciles resources.
- [Troubleshooting](../troubleshooting.md) – Fix common issues you might encounter.