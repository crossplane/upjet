#  Reverting back to Monolith Providers

1. Backup managed resource manifests:

```bash
kubectl get managed -o yaml > backup-mrs.yaml
```

2. Update deletion policy to `Orphan`:

P.S: If this field is used in the managed resources, we need to have special treatment

```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec":{"deletionPolicy":"Orphan"}}' --type=merge
```

3. Install monolith provider:

```bash
kubectl apply -f - <<EOF
apiVersion: pkg.crossplane.io/v1
kind: Provider
Metadata:
  name: $PROVIDER_NAME
spec:
  package: $PACKAGE
  revisionActivationPolicy: Manual
EOF
```

5. Delete smaller provider(s):

```bash
kubectl delete provider.pkg $(kubectl get provider.pkg |grep upbound-release-candidates |awk '{print $1}')
```

6. Update monolith providers with `revisionActivationPolicy:Automatic`:

```bash
kubectl patch provider.pkg $PROVIDER_NAME --type=merge -p='{"spec":{"revisionActivationPolicy":"Automatic"}}'
```

7. Verify that MRs and providers are ready:

```bash
kubectl get managed
kubectl get provider.pkg
```

8. Update deletion policy to `Delete`:

```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec":{"deletionPolicy":"Delete"}}' --type=merge
```