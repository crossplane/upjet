#!/bin/sh
set -e

get_smaller_providers(){
  provider="${1}"
  version="${2}"
  smaller=$(grep -irn ${provider}.upbound.io/v1beta1 "${CONF_PATH}" | awk '{print $3}'| cut -d '.' -f 1 | sort | uniq)

  for s in ${smaller[@]}; do if [ $s != "azure" ]; then echo "- provider: xpkg.upbound.io/upbound-release-candidates/provider-$provider-$s";echo  "  version:  \"$version\""; fi; done
}

providers=( "aws:v0.36.0" "gcp:v0.33.0" "azure:v0.34.0" )

for pp in "${providers[@]}"; do get_smaller_providers "${pp%%:*}" "${pp##*:}"; done