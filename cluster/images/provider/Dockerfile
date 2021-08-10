# download Terraform provider plugin binary
FROM golang:1.16.7-alpine as plugin_binary

ARG PROVIDER_NAME
ARG PROVIDER_VERSION
ARG PROVIDER_DL_VERSION=78a70965b1d642be6dee0a50d76b0030aa1002e8

RUN mkdir -p /tmp/provider && \
    go get github.com/ulucinar/terraform-provider-dl@${PROVIDER_DL_VERSION} && \
    /go/bin/terraform-provider-dl --provider-name=${PROVIDER_NAME} --version=${PROVIDER_VERSION} \
        --output=/tmp/provider

########################################################################################################################
# provider image
FROM alpine:3.14.1

COPY --from=plugin_binary /tmp/provider/* /usr/local/bin/provider
RUN apk --no-cache add socat && \
    chmod +x /usr/local/bin/provider

ADD provider-wrapper /usr/local/bin/

ENTRYPOINT ["provider-wrapper"]