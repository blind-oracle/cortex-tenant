# cortex-tenant-ns-label

This is a fork of [cortex-tenant](https://github.com/blind-oracle/cortex-tenant). It adds functionality to fetch the tenant from a K8s namespace annotation.

# cortex-tenant

[![Go Report Card](https://goreportcard.com/badge/github.com/blind-oracle/cortex-tenant)](https://goreportcard.com/report/github.com/blind-oracle/cortex-tenant)
[![Coverage Status](https://coveralls.io/repos/github/blind-oracle/cortex-tenant/badge.svg?branch=main)](https://coveralls.io/github/blind-oracle/cortex-tenant?branch=main)
[![Build Status](https://www.travis-ci.com/blind-oracle/cortex-tenant.svg?branch=main)](https://www.travis-ci.com/blind-oracle/cortex-tenant)

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

Application expects the config file at `/etc/cortex-tenant.yml` by default.

```yaml
# Where to listen for incoming write requests from Prometheus
listen: 0.0.0.0:8080

# Profiling API, remove to disable
listen_pprof: 0.0.0.0:7008

# Where to send the modified requests (Cortex/Mimir)
target: http://127.0.0.1:9091/receive

# Whether to enable querying for IPv6 records
enable_ipv6: false

# Authentication (optional)
auth:
  # Egress HTTP basic auth -> add `Authentication` header to outgoing requests
  egress:
    username: foo
    password: bar

# Log level
log_level: warn

# HTTP request timeout
timeout: 10s

# Timeout to wait on shutdown to allow load balancers detect that we're going away.
# During this period after the shutdown command the /alive endpoint will reply with HTTP 503.
# Set to 0s to disable.
timeout_shutdown: 10s

# Max number of parallel incoming HTTP requests to handle
concurrency: 10

# Whether to forward metrics metadata from Prometheus to Cortex/Mimir
# Since metadata requests have no timeseries in them - we cannot divide them into tenants
# So the metadata requests will be sent to the default tenant only, if one is not defined - they will be dropped
metadata: false

# If true response codes from metrics backend will be logged to stdout. This setting can be used to suppress errors
# which can be quite verbose like 400 code - out-of-order samples or 429 on hitting ingestion limits
# Also, those are already reported by other services like Cortex/Mimir distributors and ingesters
log_response_errors: true

# Maximum duration to keep outgoing connections alive (to Cortex/Mimir)
# Useful for resetting L4 load-balancer state
# Use 0 to keep them indefinitely
max_connection_duration: 0s

tenant:
  # Which label to look for the tenant information
  label: tenant

  # Whether to remove the tenant label from the request
  label_remove: true
  
  # To which header to add the tenant ID
  header: X-Scope-OrgID

  # Which tenant ID to use if the label is missing in any of the timeseries
  # If this is not set or empty then the write request with missing tenant label
  # will be rejected with HTTP code 400
  default: foobar

  # Enable if you want all metrics from Prometheus to be accepted with a 204 HTTP code
  # regardless of the response from upstream. This can lose metrics if Cortex/Mimir is
  # throwing rejections.
  accept_all: false
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

`deploy/k8s` directory contains the deployment, service and configmap manifest files for deploying this on Kubernetes. You can overwrite the default config by editing the configuration parameters in the configmap manifest.

```bash
kubectl apply -f deploy/k8s/cortex-tenant-deployment.yaml
kubectl apply -f deploy/k8s/cortex-tenant-service.yaml
kubectl apply -f deploy/k8s/config-file-configmap.yml
```
