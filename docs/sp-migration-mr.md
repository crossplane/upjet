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
version_aws=v0.37.0
version_azure=v0.34.0
version_gcp=v0.34.0

rm -f "sp-manual.yaml" && touch "sp-manual.yaml"
rm -f "sp-family-manual.yaml" && touch "sp-family-manual.yaml"
kubectl get managed --no-headers -o jsonpath='{range .items[*]}{.apiVersion}{"\n"}{end}' | grep -E '(aws|gcp|azure).upbound.io' | sort | uniq | while read -r line
do
  service=$(echo "${line}" | cut -d. -f1)
  provider=$(echo "${line}" | cut -d. -f2)
  if [ "${provider}" = "upbound" ]; then
    # azure.upbound.io is an exception where apiVersion does not contain the service name
    # we have those resources in the family package
    provider="azure"
  fi
  eval version=\$"version_${provider}"

  for sp in config ${service}; do
    filename="sp-manual.yaml"
    providername="${provider}-${sp}"
    
    if [ "${sp}" = "config" ] || [ "${sp}" = "azure" ]; then
      # azure.upbound.io is an exception where apiVersion does not contain the service name
      # we have those resources in the family package
      providername="family-$provider"
      filename="sp-family-manual.yaml"
    fi
    if ! cat "${filename}" | grep provider-${providername}:${version} > /dev/null; then
    echo "apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: upbound-release-candidates-provider-${providername}
spec:
  package: xpkg.upbound.io/upbound-release-candidates/provider-${providername}:${version}
  revisionActivationPolicy: Manual" >> "${filename}"
    echo "---" >> "${filename}"
    fi
  done
done
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