# cortex-tenant

Prometheus remote write proxy which marks timeseries with a Cortex tenant ID based on labels.

## Overview

Cortex tenants (separate buckets where metrics are injected to and queried from) are identified by a `X-Scope-OrgID` HTTP header on both writes and queries.

This makes it impossible to use a single Prometheus (or an HA pair) to write to multiple tenants.

This proxy solves this problem. The logic is:

- Receives Prometheus remote write
- Searches all timeseries for a specific label and gets a tenant ID from its value
  If the label wasn't not found then it uses configured default tenant name (`default`)
- Optionally removes this label
- Groups timeseries by tenant
- Issues a number of per-tenant HTTP requests to Cortex adding the tenant HTTP header (`X-Scope-OrgID` by default)
