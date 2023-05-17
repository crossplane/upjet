#  Managed Resource Migration to Smaller Providers

1. Backup managed resources:

```bash
kubectl get managed -o yaml > backup-mrs.yaml
```

2. Update deletion policy to `Orphan`:

P.S: If this field is used in the managed resources, we need to have special treatment

```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec":{"deletionPolicy":"Orphan"}}' --type=merge
```

3. Generate smaller provider manifests

```bash
export KUBECONFIG=<path of the Kubeconfig file>
./generate-manifests.sh
```

4. Install family providers with `revisionActivationPolicy: Manual`:

Verify that `sp-family-manual.yaml` files are generated with the correct content

```bash
cat sp-family-manual.yaml
```

Install the family provider(s)

```bash
kubectl apply -f sp-family-manual.yaml
```

Make sure the family provider(s) are in `Installed: False` and `Healthy: True` state:

```bash
kubectl get providers.pkg
```

5. Install smaller providers with `revisionActivationPolicy: Manual`:


Verify that `sp-manual.yaml` files are generated with the correct content

```bash
cat sp-manual.yaml
```

Install the smallers providers

```bash
kubectl apply -f sp-manual.yaml
```

Make sure the smaller providers are in `Installed: False` and `Healthy: True` state:

```bash
kubectl get providers.pkg
```

6. Delete monolith provider(s):

```bash
kubectl delete provider.pkg $PROVIDER_NAME
```

7. Update smaller providers with `revisionActivationPolicy:Automatic`:

```bash
sed 's/revisionActivationPolicy: Manual/revisionActivationPolicy: Automatic/' sp-family-manual.yaml > sp-family-automatic.yaml

kubectl apply -f sp-family-automatic.yaml
```


```bash
sed 's/revisionActivationPolicy: Manual/revisionActivationPolicy: Automatic/' sp-manual.yaml > sp-automatic.yaml

kubectl apply -f sp-automatic.yaml
```

8. Verify that MRs and providers are ready:

```bash
kubectl get managed
kubectl get provider.pkg
```

9. Update deletion policy to `Delete`:

```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec":{"deletionPolicy":"Delete"}}' --type=merge
```