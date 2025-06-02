FROM golang:latest AS builder

ARG TARGETOS
ARG TARGETARCH

ENV GO111MODULE=on \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    CGO_ENABLED=0

WORKDIR /build

# Cache modules
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN OS=${TARGETOS} ARCH=${TARGETARCH} make build

WORKDIR /dist

RUN cp /build/cortex-tenant ./cortex-tenant
RUN mkdir /data && cp /build/deploy/cortex-tenant.yml /data/cortex-tenant.yml

FROM scratch

COPY --chown=65534:0 --from=builder /dist /

COPY --chown=65534:0 --from=builder /data /data
USER 65534

WORKDIR /data

COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/cortex-tenant"]
CMD ["-config", "/data/cortex-tenant.yml"]
