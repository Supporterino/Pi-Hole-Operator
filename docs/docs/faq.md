# Frequently Asked Questions

| Question | Answer |
|----------|--------|
| **Can I expose Pi‑Hole via a LoadBalancer?** | Yes. Set `spec.service.type: LoadBalancer` in the Helm values or the CR. The operator will create a Service of that type and expose port 53/UDP. |
| **How do I upgrade the operator?** | Use Helm: `helm repo update` then `helm upgrade pi-hole-operator oci://ghcr.io/supporterino/pi-hole-operator/helm/pi-hole-operator --namespace pi-hole-operator-system`. |
| **Where are the Pi‑Hole logs stored?** | They’re in the pod logs. Use `kubectl logs deployment/<pihole-cluster> -n pi-hole-operator-system`. For persistent storage, mount a PVC to `/etc/pihole` in the Deployment. |
| **Can I use custom ad‑lists?** | Yes. Add URLs under `spec.config.adLists` in the `PiHoleCluster` CR. The operator will fetch and sync them every 5 minutes (configurable). |
| **Is cert‑manager required for TLS?** | No. Ingress TLS is optional; if you set `ingress.enabled=true` and have cert‑manager installed, the operator will request a certificate automatically. Without cert‑manager you can still use Ingress but must provide your own TLS secret. |
| **Does the operator support StatefulSets?** | Currently it uses a Deployment with read‑only replicas. If you need stateful behavior, mount the same PVC to all pods; the operator already supports a shared PVC via `spec.persistence`. |
| **How can I contribute?** | Fork the repo, create a feature branch, run `make test` and open a PR. See our [Contribution Guide](../CONTRIBUTING.md). |