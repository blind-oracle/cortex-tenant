FROM docker.io/library/golang:1.20 as builder

WORKDIR /build

# Cache modules
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN make build

FROM busybox
COPY --from=builder /build/cortex-tenant-ns-label /
COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
CMD ["/bin/sh", "-c", "/bin/echo \"${CONFIG}\" > /tmp/cortex-tenant.yml; /cortex-tenant-ns-label -config /tmp/cortex-tenant.yml"]
