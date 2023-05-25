# Finding Configuration Dependencies

If you want to update your configuration dependencies with smaller providers, you can use the `find-dependencies.sh` script.
That script requires the path of the configuration files as an environment variable and traverse the composition files
to find which smaller providers are required by the configuration. It outputs a yaml
list of the smaller providers with corresponding versions and that list can be copy-pasted to the `crossplane.yaml` file
directly.

```bash
export CONF_PATH=<root path of the configuration files>
./find-dependencies.sh
```