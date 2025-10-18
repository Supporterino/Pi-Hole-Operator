# Troubleshooting

Below are the most common issues you might encounter, along with quick diagnostics and solutions.

## 1️⃣ Operator pod stuck in `CrashLoopBackOff`

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `CrashLoopBackOff` in `pi-hole-operator-controller-manager` | Missing or invalid cert‑manager webhook configuration | Ensure `certmanager.enable=true` in Helm values and that cert‑manager is running. |
| Crash after startup | Missing environment variables or secrets | Verify `apiPassword` secret exists and is referenced correctly. |
| Crash due to image pull error | Wrong image tag or registry auth | Use `helm upgrade` with the correct OCI image reference. |

**Command**

```bash
kubectl logs deployment/pi-hole-operator-controller-manager -n pi-hole-operator-system
```

## 2️⃣ Pi‑Hole pods not starting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| Pods in `ImagePullBackOff` | Registry credentials missing | Add Docker config to the service account or use image pull secrets. |
| Pods crash with `exit status 1` | Invalid config (e.g., missing TZ) | Check the ConfigMap or env vars; update the CR. |
| Pods stuck in `Pending` | Insufficient resources / PVC not bound | Verify node capacity and that the PVC is correctly defined. |

**Command**

```bash
kubectl describe pod <pihole-pod> -n pi-hole-operator-system
```

## 3️⃣ Ingress not reachable

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| Ingress shows `ADDRESS` but DNS not resolving | DNS record missing or incorrect | Add a CNAME/ALIAS pointing to the Ingress controller’s IP. |
| TLS handshake fails | cert‑manager not issuing cert | Check `cert-manager.io/cluster-issuer` annotation and cert‑manager logs. |
| HTTP 404 on `/metrics` | Service selector mismatch | Verify the service selector matches the Pi‑Hole pods. |

**Command**

```bash
kubectl describe ingress <ingress-name> -n pi-hole-operator-system
```

## 4️⃣ Metrics not exposed

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `/metrics` returns 404 | Metrics sidecar not deployed or wrong port | Ensure `metrics.enabled=true` in Helm values. |
| Prometheus scrape fails | ServiceMonitor not created | Verify `metrics.serviceMonitor.enabled=true` and that the Prometheus instance is configured to scrape it. |

**Command**

```bash
kubectl port-forward svc/pi-hole-metrics 9100 -n pi-hole-operator-system
curl http://localhost:9100/metrics
```

## 5️⃣ Ad‑lists not syncing

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| No new entries in Pi‑Hole UI | `spec.sync.adLists=false` or URL unreachable | Set `sync.adLists=true` and ensure URLs are reachable from the operator pod. |
| Sync errors in logs | Authentication or network issue | Check logs: `kubectl logs deployment/pi-hole-operator-controller-manager`. |

---

**General debugging**

```bash
# Operator logs
kubectl logs deployment/pi-hole-operator-controller-manager -n pi-hole-operator-system

# Pi‑Hole pod logs
kubectl logs deployment/<pihole-cluster> -n pi-hole-operator-system

# Inspect CR status
kubectl get piholecluster <name> -o yaml | grep -A5 status
```

If you can’t resolve the issue, feel free to open an issue on GitHub with the relevant logs and a description of what you’ve tried.