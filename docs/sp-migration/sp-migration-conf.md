
# Configuration Migration to Smaller Providers

1. Backup managed resource, composite and claim manifests:

```bash
kubectl get managed -o yaml > backup-mrs.yaml
kubectl get composite -o yaml > backup-composites.yaml
kubectl get claim --all-namespaces -o yaml > backup-claims.yaml
```

2. Update deletion policy to `Orphan`:

P.S: If this field is used in the composition files, we need to have special treatment

```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec":{"deletionPolicy":"Orphan"}}' --type=merge
```

3. Generate smaller provider manifests

```bash
export KUBECONFIG=<path of the Kubeconfig file>
./generate-manifests.sh
```

Alternatively, you can generate the provider manifests out of the local
Configuration files

```bash
export CONF_PATH=<root path of the configuration files>
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

6. Remove monolith provider(s) from dependsOn. Build/push and update the configuration

7. Delete monolith provider(s):

```bash
kubectl delete provider.pkg $PROVIDER_NAME
```

8. Update smaller providers with `revisionActivationPolicy:Automatic`:

```bash
sed 's/revisionActivationPolicy: Manual/revisionActivationPolicy: Automatic/' sp-family-manual.yaml > sp-family-automatic.yaml

kubectl apply -f sp-family-automatic.yaml
```


```bash
sed 's/revisionActivationPolicy: Manual/revisionActivationPolicy: Automatic/' sp-manual.yaml > sp-automatic.yaml

kubectl apply -f sp-automatic.yaml
```

9. Verify that MRs and providers are ready:

```bash
kubectl get managed
kubectl get provider.pkg
```

10. Find new dependencies and add them to `dependsOn` field in the `crossplane.yaml` file:

```bash
export CONF_PATH=<root path of the configuration files>
./find-dependencies.sh
```

12. Build/push/update the configuration to the new version

13. Update deletion policy to `Delete`:

```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec":{"deletionPolicy":"Delete"}}' --type=merge
```
