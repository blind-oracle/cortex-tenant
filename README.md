# cortex-tenant

[![Go Report Card](https://goreportcard.com/badge/github.com/blind-oracle/cortex-tenant)](https://goreportcard.com/report/github.com/blind-oracle/cortex-tenant)
[![Coverage Status](https://coveralls.io/repos/github/blind-oracle/cortex-tenant/badge.svg?branch=main)](https://coveralls.io/github/blind-oracle/cortex-tenant?branch=main)
[![Build Status](https://www.travis-ci.com/blind-oracle/cortex-tenant.svg?branch=main)](https://www.travis-ci.com/blind-oracle/cortex-tenant)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cortex-tenant)](https://artifacthub.io/packages/helm/cortex-tenant/cortex-tenant)

Prometheus remote write proxy which marks timeseries with a Cortex/Mimir tenant ID based on labels.

## Architecture

![Architecture](architecture.svg)

## Overview

Cortex/Mimir tenants (separate namespaces where metrics are stored to and queried from) are identified by `X-Scope-OrgID` HTTP header on both writes and queries.

~~Problem is that Prometheus can't be configured to send this header~~ Actually in some recent version (year 2021 onwards) this functionality was added, but the tenant is the same for all jobs. This makes it impossible to use a single Prometheus (or an HA pair) to write to multiple tenants.

This software solves the problem using the following logic:

- Receive Prometheus remote write
- Search each timeseries for a specific label name and extract a tenant ID from its value.
  If the label wasn't found then it can fall back to a configurable default ID.
  If none is configured then the write request will be rejected with HTTP code 400
- Optionally removes this label from the timeseries
- Groups timeseries by tenant
- Issues a number of parallel per-tenant HTTP requests to Cortex/Mimir with the relevant tenant HTTP header (`X-Scope-OrgID` by default)

## Usage

- Get `rpm` or `deb` for amd64 from the Releases page. For building see below.

### HTTP Endpoints

- GET `/alive` returns 200 by default and 503 if the service is shutting down (if `timeout_shutdown` setting is > 0)
- POST `/push` receives metrics from Prometheus - configure remote write to send here

### Configuration

The service can be configured by a config file and/or environment variables. Config file may be specified by passing `-config` CLI argument.

If both are used then the env vars have precedence (i.e. they override values from config).
See below for config file format and corresponding env vars.

```yaml
# Where to listen for incoming write requests from Prometheus
# env: CT_LISTEN
listen: 0.0.0.0:8080

# Profiling API, remove to disable
# env: CT_LISTEN_PPROF
listen_pprof: 0.0.0.0:7008

# Where to send the modified requests (Cortex/Mimir)
# env: CT_TARGET
target: http://127.0.0.1:9091/receive

# Whether to enable querying for IPv6 records
# env: CT_ENABLE_IPV6
enable_ipv6: false

# This parameter sets the limit for the count of outgoing concurrent connections to Cortex / Mimir.
# By default it's 64 and if all of these connections are busy you will get errors when pushing from Prometheus.
# If your `target` is a DNS name that resolves to several IPs then this will be a per-IP limit.
# env: CT_MAX_CONNS_PER_HOST
max_conns_per_host: 0

# Authentication (optional)
auth:
  # Egress HTTP basic auth -> add `Authentication` header to outgoing requests
  egress:
    # env: CT_AUTH_EGRESS_USERNAME
    username: foo
    # env: CT_AUTH_EGRESS_PASSWORD
    password: bar

# Log level
# env: CT_LOG_LEVEL
log_level: warn

# HTTP request timeout
# env: CT_TIMEOUT
timeout: 10s

# Timeout to wait on shutdown to allow load balancers detect that we're going away.
# During this period after the shutdown command the /alive endpoint will reply with HTTP 503.
# Set to 0s to disable.
# env: CT_TIMEOUT_SHUTDOWN
timeout_shutdown: 10s

# Max number of parallel incoming HTTP requests to handle
# env: CT_CONCURRENCY
concurrency: 10

# Whether to forward metrics metadata from Prometheus to Cortex/Mimir
# Since metadata requests have no timeseries in them - we cannot divide them into tenants
# So the metadata requests will be sent to the default tenant only, if one is not defined - they will be dropped
# env: CT_METADATA
metadata: false

# If true response codes from metrics backend will be logged to stdout. This setting can be used to suppress errors
# which can be quite verbose like 400 code - out-of-order samples or 429 on hitting ingestion limits
# Also, those are already reported by other services like Cortex/Mimir distributors and ingesters
# env: CT_LOG_RESPONSE_ERRORS
log_response_errors: true

# Maximum duration to keep outgoing connections alive (to Cortex/Mimir)
# Useful for resetting L4 load-balancer state
# Use 0 to keep them indefinitely
# env: CT_MAX_CONN_DURATION
max_connection_duration: 0s

# Address where metrics are available
# env: CT_LISTEN_METRICS_ADDRESS
listen_metrics_address: 0.0.0.0:9090

# If true, then a label with the tenant’s name will be added to the metrics
# env: CT_METRICS_INCLUDE_TENANT
metrics_include_tenant: true

tenant:
  # Which label to look for the tenant information
  # env: CT_TENANT_LABEL
  label: tenant

  # Whether to remove the tenant label from the request
  # env: CT_TENANT_LABEL_REMOVE
  label_remove: true
  
  # To which header to add the tenant ID
  # env: CT_TENANT_HEADER
  header: X-Scope-OrgID

  # Which tenant ID to use if the label is missing in any of the timeseries
  # If this is not set or empty then the write request with missing tenant label
  # will be rejected with HTTP code 400
  # env: CT_TENANT_DEFAULT
  default: foobar

  # Enable if you want all metrics from Prometheus to be accepted with a 204 HTTP code
  # regardless of the response from upstream. This can lose metrics if Cortex/Mimir is
  # throwing rejections.
  # env: CT_TENANT_ACCEPT_ALL
  accept_all: false

  # Optional prefix to be added to a tenant header before sending it to Cortex/Mimir.
  # Make sure to use only allowed characters:
  # https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/
  # env: CT_TENANT_PREFIX
  prefix: foobar-
```

### Prometheus configuration example

```yaml
remote_write:
  - name: cortex_tenant
    url: http://127.0.0.1:8080/push

scrape_configs:
  - job_name: job1
    scrape_interval: 60s
    static_configs:
      - targets:
          - target1:9090
        labels:
          tenant: foobar

  - job_name: job2
    scrape_interval: 60s
    static_configs:
      - targets:
          - target2:9090
        labels:
          tenant: deadbeef
```

This would result in `job1` metrics ending up in the `foobar` tenant in Cortex/Mimir and `job2` in `deadbeef`.

## Building

`make build` should create you an _amd64_ binary.

If you want `deb` or `rpm` packages then install [FPM](https://fpm.readthedocs.io) and then run `make rpm` or `make deb` to create the packages.

## Containerization

To use the current container you need to overwrite the default configuration file, mount your configuration into to `/data/cortex-tenant.yml`.

You can overwrite the default config by starting the container with:

```bash
docker container run \
-v <CONFIG_LOCATION>:/data/cortex-tenant.yml \
ghcr.io/blind-oracle/cortex-tenant:1.6.1
```

... or build your own Docker image:

```Dockerfile
FROM ghcr.io/blind-oracle/cortex-tenant:1.6.1
ADD my-config.yml /data/cortex-tenant.yml
```

### Deploy on Kubernetes

#### Using manifests

`deploy/k8s/manifests` directory contains the deployment, service and configmap manifest files for deploying this on Kubernetes. You can overwrite the default config by editing the configuration parameters in the configmap manifest.

```bash
kubectl apply -f deploy/k8s/manifests/cortex-tenant-deployment.yaml
kubectl apply -f deploy/k8s/manifests/cortex-tenant-service.yaml
kubectl apply -f deploy/k8s/manifests/config-file-configmap.yml
```

#### Using a Helm Chart

`deploy/k8s/chart` directory contains a chart for deploying this on Kubernetes. You can use `deploy/k8s/chart/testing` directory to test the deployment using helmfile.

```bash
helmfile -f deploy/k8s/chart/testing/helmfile.yaml template
helmfile -f deploy/k8s/chart/testing/helmfile.yaml apply
```
