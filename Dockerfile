FROM golang:stretch as builder

ENV GO111MODULE=on \
    CGO_ENABLED=0

WORKDIR /build

# Cache modules
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN make build

WORKDIR /dist

RUN cp /build/cortex-tenant ./cortex-tenant

RUN ldd cortex-tenant | tr -s '[:blank:]' '\n' | grep '^/' | \
    xargs -I % sh -c 'mkdir -p $(dirname ./%); cp % ./%;'
RUN mkdir -p lib64 && cp /lib64/ld-linux-x86-64.so.2 lib64/

RUN mkdir /data && cp /build/deploy/cortex-tenant.yml /data/cortex-tenant.yml

FROM scratch

ENV CONFIG_FILE cortex-tenant.yml
COPY --chown=65534:0 --from=builder /dist /

COPY --chown=65534:0 --from=builder /data /data
USER 65534

WORKDIR /data

COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/cortex-tenant"]
CMD ["-config", "/data/cortex-tenant.yml"]
