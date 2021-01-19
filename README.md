# cortex-tenant

[![Go Report Card](https://goreportcard.com/badge/github.com/blind-oracle/cortex-tenant)](https://goreportcard.com/report/github.com/blind-oracle/cortex-tenant)
[![Coverage Status](https://coveralls.io/repos/github/blind-oracle/cortex-tenant/badge.svg?branch=main)](https://coveralls.io/github/blind-oracle/cortex-tenant?branch=main)
[![Build Status](https://www.travis-ci.com/blind-oracle/cortex-tenant.svg?branch=main)](https://www.travis-ci.com/blind-oracle/cortex-tenant)

Prometheus remote write proxy which marks timeseries with a Cortex tenant ID based on labels.

## Overview

Cortex tenants (separate namespaces where metrics are stored to and queried from) are identified by a `X-Scope-OrgID` HTTP header on both writes and queries.

This makes it impossible to use a single Prometheus (or an HA pair) to write to multiple tenants.

This proxy solves the problem:

- Receive Prometheus remote write
- Search each timeseries for a specific label name and extract a tenant ID from its value.
  If the label wasn't not found then it can fall back to a configurable default ID.
  If none is configured then the write request will be rejected.
- Optionally removes this label the timeseries
- Groups timeseries by tenant
- Issues a number of parallel per-tenant HTTP requests to Cortex adding the tenant HTTP header (`X-Scope-OrgID` by default)

## Usage

- Get `rpm` or `deb` for amd64 from the Releases page. For building see below.

### Configuration

Application expects the config file at `/etc/cortex-tenant.yml` by default.

```yaml
# Where to listen for write requests
listen: 0.0.0.0:8080
# Profiling API, disabled if ommited
listen_pprof: 0.0.0.0:7008
# Where to send the modified requests (Cortex)
target: http://127.0.0.1:9091/receive
# Log level
log_level: warn
# HTTP request timeout
timeout: 10s
# Timeout to wait on shutdown to allow load balancers detect that we're going away
# During this period the /alive endpoint will reply with HTTP 503
# Set to 0s to disable
timeout_shutdown: 10s

tenant:
  # Which label to look for the tenant information
  label: tenant
  # Whether to remove the tenant label from the request
  label_remove: true
  # To which header to add the tenant ID
  header: X-Scope-OrgID
  # Which tenant ID to use if the label is missing in any of the timeseries
  # If this is not set or empty then the write request will be rejected with HTTP code 400
  default: foobar
```

## Building

`make build` should create you an _amd64_ binary.

If you want `deb` or `rpm` packages then install [FPM](https://fpm.readthedocs.io) and then run `make rpm` or `make deb` to create the packages.
