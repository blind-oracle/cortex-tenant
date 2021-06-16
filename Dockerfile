FROM --platform=linux/amd64 golang:latest as builder
WORKDIR /go/src/app
COPY ./project/cortex-tenant .
RUN make build

FROM scratch
COPY --from=builder /go/src/app/cortex-tenant /cortex-tenant
COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT [ "/cortex-tenant" ]
