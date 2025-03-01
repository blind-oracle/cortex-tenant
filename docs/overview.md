# Architecture

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
