# Configuration

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
backend:
  url: http://127.0.0.1:9091/receive
  # Authentication (optional)
  auth:
    # Egress HTTP basic auth -> add `Authentication` header to outgoing requests
    egress:
      # env: CT_AUTH_EGRESS_USERNAME
      username: foo
      # env: CT_AUTH_EGRESS_PASSWORD
      password: bar

# Whether to enable querying for IPv6 records
# env: CT_ENABLE_IPV6
enable_ipv6: false

# This parameter sets the limit for the count of outgoing concurrent connections to Cortex / Mimir.
# By default it's 64 and if all of these connections are busy you will get errors when pushing from Prometheus.
# If your `target` is a DNS name that resolves to several IPs then this will be a per-IP limit.
# env: CT_MAX_CONNS_PER_HOST
max_conns_per_host: 0

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
  # List of labels examined for tenant information.
  # env: CT_TENANT_LABEL_LIST
  label_list:
    - tenant
    - other_tenant

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

  # If true will use the tenant ID of the inbound request as the prefix of the new tenant id.
  # Will be automatically suffixed with a `-` character.
  # Example:
  #   Prometheus forwards metrics with `X-Scope-OrgID: Prom-A` set in the inbound request.
  #   This would result in the tenant prefix being set to `Prom-A-`.
  # https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/
  # env: CT_TENANT_PREFIX_PREFER_SOURCE
  prefix_prefer_source: false
```
