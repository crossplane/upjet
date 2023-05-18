
# Migration to Smaller Providers

1. Backup managed resource, composite and claim manifests:

```bash
kubectl get managed -o yaml > backup-mrs.yaml
kubectl get composite -o yaml > backup-composites.yaml
kubectl get claim --all-namespaces -o yaml > backup-claims.yaml
```

2. Update deletion policy to `Orphan`:
P.S: If this field is used in the managed resources, we need to have special treatment

```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec":{"deletionPolicy":"Orphan"}}' --type=merge
```

3. Generate smaller provider manifests with the script:

```bash
export KUBECONFIG=<path of the Kubeconfig file>
./generate-manifests.sh
```

Alternatively, you can generate the provider manifests out of the local
Configuration files. No runtime Managed Resources will be included in this case.

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

6. If you installed monolith provider(s) as a configuration dependency; set `spec.skipDependencyResoultion: true` and 
remove the configuration dependency from the lock resource to prevent reinstallation of the monolith provider(s).

```bash
kubectl patch configuration.pkg $CONFIGURATION_NAME -p '{"spec":{"skipDependencyResolution": true}}' --type=merge

kubectl edit lock lock
# remove `packages` array item where `type: Configuration` and `dependencies[0].package is the monolith provider
# example lock resources can be found docs/sp-migration/example-lock-before.yaml and docs/sp-migration/example-lock-after.yaml
```

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

10. If you want to add smaller providers to the configuration's `dependsOn` list, please follow the [guide] and build/push/update
the configuration to the new version. Once the configuration is updated, change `skipDependencyResolution` to `false` again:

```bash
kubectl patch configuration.pkg $CONFIGURATION_NAME -p '{"spec":{"skipDependencyResolution": false}}' --type=merge
```

11. Update deletion policy to `Delete`:
P.S: If this field is used in the managed resources, we need to have special treatment

```bash
kubectl patch $(kubectl get managed -o name) -p '{"spec":{"deletionPolicy":"Delete"}}' --type=merge
```

[guide]: configuration-dependencies.md