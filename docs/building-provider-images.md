# Building Provider Images Locally for Family Providers

1. Check out the provider repo, e.g., upbound/provider-gcp, and go to the project
directory on your local machine.

2. Do the necessary changes locally and please make sure you have comitted all of them.

3. Run `make load-pkg` as follows.

    a. If you want to build the monolithic provider image:

     ```bash
    make load-pkg
    ```

    After the command is completed, you can see `provider-gcp-monolith-arm64` and `provider-gcp-monolith-amd64` images:
    ```
    > docker images
    REPOSITORY                          TAG                        IMAGE ID       CREATED        SIZE
    build-38f30a9b/provider-gcp-arm64   latest                     d475fe99b058   13 hours ago   139MB
    provider-gcp-monolith-arm64         v0.36.0-rc.0.9.g6940237d   81978975bf51   13 hours ago   236MB
    build-38f30a9b/provider-gcp-amd64   latest                     15fe58d7198f   13 hours ago   146MB
    provider-gcp-monolith-amd64         v0.36.0-rc.0.9.g6940237d   1891be45f2ee   13 hours ago   246MB
    ```

    b. If you want to build the subpackages, for instance for provider-family-gcp you need `cloudplatform`, `container`, `dns` packages:

     ```bash
    make load-pkg SUBPACKAGES="config cloudplatform container dns"
    ```
    *Note: You should always include the `config` package in the subpackages list as it is a dependency to all family provider packages.*

    After the command is completed, you can see the provider images, e.g. `provider-gcp-dns-arm64`:
    ```
    > docker images
    REPOSITORY                          TAG                        IMAGE ID       CREATED        SIZE
    provider-gcp-dns-arm64              v0.36.0-rc.0.9.g6940237d   ab4269482718   13 hours ago   216MB
    build-38f30a9b/provider-gcp-arm64   latest                     d475fe99b058   13 hours ago   139MB
    provider-gcp-container-arm64        v0.36.0-rc.0.9.g6940237d   6b6791a6659b   13 hours ago   216MB
    provider-gcp-cloudplatform-arm64    v0.36.0-rc.0.9.g6940237d   55a9798fd10c   13 hours ago   217MB
    provider-gcp-config-arm64           v0.36.0-rc.0.9.g6940237d   4426b77cea2c   13 hours ago   215MB
    provider-gcp-dns-amd64              v0.36.0-rc.0.9.g6940237d   bf592eaae5de   13 hours ago   225MB
    provider-gcp-config-amd64           v0.36.0-rc.0.9.g6940237d   ae5affc2cac7   13 hours ago   224MB
    provider-gcp-cloudplatform-amd64    v0.36.0-rc.0.9.g6940237d   d6eeb6e43e95   13 hours ago   226MB
    build-38f30a9b/provider-gcp-amd64   latest                     15fe58d7198f   13 hours ago   146MB
    provider-gcp-container-amd64        v0.36.0-rc.0.9.g6940237d   beb71a5d733e   13 hours ago   226MB
    ```

## Running Providers with Locally Built Images

One way to install locally built images is publishing them to Dockerhub and installing from there. For example, if you want to install the `cloudplatform` package on a amd64 arch:

1. Tag and publish provider images to Dockerhub:

    ```bash
    # tag the images
    docker tag provider-gcp-config-amd64:v0.36.0-rc.0.9.g6940237d <your-dockerhub-org>/provider-gcp-config-amd64:v0.36.0-rc.0.9.g6940237d
    docker tag provider-gcp-cloudplatform-amd64:v0.36.0-rc.0.9.g6940237d <your-dockerhub-org>/provider-gcp-cloudplatform-amd64:v0.36.0-rc.0.9.g6940237d
    # push the images to your dockerhub org
    docker push <your-dockerhub-org>/provider-gcp-config-amd64:v0.36.0-rc.0.9.g6940237d
    docker push <your-dockerhub-org>/provider-gcp-cloudplatform-amd64:v0.36.0-rc.0.9.g6940237d
    ```

2. Install the provider packages using the following yaml:

    ```yaml
    apiVersion: pkg.crossplane.io/v1
    kind: Provider
    metadata:
        name: upbound-provider-gcp-config
    spec:
        package: turkenf/provider-gcp-config-amd64:v0.36.0-rc.0.9.g6940237d
        skipDependencyResolution: true
    ---
    apiVersion: pkg.crossplane.io/v1
    kind: Provider
    metadata:
        name: upbound-provider-gcp-cloudplatform
    spec:
        package: turkenf/provider-gcp-cloudplatform-amd64:v0.36.0-rc.0.9.g6940237d
        skipDependencyResolution: true
    ```

3. Verify that providers are installed and healthy:

```bash
> kubectl get providers.pkg.crossplane.io
NAME                                 INSTALLED   HEALTHY   PACKAGE                                                             AGE
upbound-provider-gcp-cloudplatform   True        True      turkenf/provider-gcp-cloudplatform-amd64:v0.36.0-rc.0.9.g6940237d   3m46s
upbound-provider-gcp-config          True        True      turkenf/provider-gcp-config-amd64:v0.36.0-rc.0.9.g6940237d          3m46s
```