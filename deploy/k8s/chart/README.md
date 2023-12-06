# cortex-tenant

![Version: 0.4.0](https://img.shields.io/badge/Version-0.4.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 1.11.0](https://img.shields.io/badge/AppVersion-1.11.0-informational?style=flat-square)

A Helm Chart for cortex-tenant

## Source Code

* <https://github.com/blind-oracle/cortex-tenant>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | [Affinity and anti-affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity) |
| annotations | object | `{}` | Annotations for deployment |
| autoscaling.enabled | bool | `true` | If enabled, HorizontalPodAutoscaler resources are created |
| autoscaling.maxReplica | int | `3` | Max number of pod replica autoscaled |
| autoscaling.minReplica | int | `1` | Min number of pod replica autoscaled |
| autoscaling.targetCPUUtilizationPercentage | int | `50` | Target CPU utilization percentage for autoscaling |
| autoscaling.targetMemoryAverageValue | string | `"100Mi"` | Target memory average value for autoscaling |
| config.auth.enabled | bool | `false` | Egress HTTP basic auth -> add `Authentication` header to outgoing requests |
| config.auth.existingSecret | string | `nil` | Secret should pass the `CT_AUTH_EGRESS_USERNAME` and `CT_AUTH_EGRESS_PASSWORD` env variables |
| config.auth.password | string | `nil` | Password (env: `CT_AUTH_EGRESS_PASSWORD`) |
| config.auth.username | string | `nil` | Username (env: `CT_AUTH_EGRESS_USERNAME`) |
| config.concurrency | int | `1000` | Max number of parallel incoming HTTP requests to handle (env: `CT_CONCURRENCY`) |
| config.enable_ipv6 | bool | `false` | Whether to enable querying for IPv6 records (env: `CT_ENABLE_IPV6`) |
| config.listen | string | `"0.0.0.0:8080"` | Where to listen for incoming write requests from Prometheus (env: `CT_LISTEN`) |
| config.listen_pprof | string | `"0.0.0.0:7008"` | Profiling API, leave empty to disable (env: `CT_LISTEN_PPROF`) |
| config.log_level | string | `"warn"` | Log level (env: `CT_LOG_LEVEL`) |
| config.log_response_errors | bool | `true` | If true response codes from metrics backend will be logged to stdout. This setting can be used to suppress errors which can be quite verbose like 400 code - out-of-order samples or 429 on hitting ingestion limits Also, those are already reported by other services like Cortex/Mimir distributors and ingesters (env: `CT_LOG_RESPONSE_ERRORS`) |
| config.max_connection_duration | string | `"0s"` | Maximum duration to keep outgoing connections alive (to Cortex/Mimir) Useful for resetting L4 load-balancer state Use 0 to keep them indefinitely (env: `CT_MAX_CONN_DURATION`) |
| config.metadata | bool | `false` | Whether to forward metrics metadata from Prometheus to Cortex Since metadata requests have no timeseries in them - we cannot divide them into tenants So the metadata requests will be sent to the default tenant only, if one is not defined - they will be dropped (env: `CT_METADATA`) |
| config.target | string | `"http://cortex-distributor.cortex.svc:8080/api/v1/push"` | Where to send the modified requests (Cortex) (env: `CT_TARGET`) |
| config.tenant.accept_all | bool | `false` | Enable if you want all metrics from Prometheus to be accepted with a 204 HTTP code regardless of the response from Cortex. This can lose metrics if Cortex is throwing rejections. (env: `CT_TENANT_ACCEPT_ALL`) |
| config.tenant.default | string | `"cortex-tenant-default"` | Which tenant ID to use if the label is missing in any of the timeseries If this is not set or empty then the write request with missing tenant label will be rejected with HTTP code 400 (env: `CT_TENANT_DEFAULT`) |
| config.tenant.header | string | `"X-Scope-OrgID"` | To which header to add the tenant ID (env: `CT_TENANT_HEADER`) |
| config.tenant.label | string | `"tenant"` | Which label to look for the tenant information (env: `CT_TENANT_LABEL`) |
| config.tenant.label_remove | bool | `false` | Whether to remove the tenant label from the request (env: `CT_TENANT_LABEL_REMOVE`) |
| config.tenant.prefix | string | `""` | Optional hard-coded prefix with delimeter for all tenant values. Delimeters allowed for use: https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/ (env: `CT_TENANT_PREFIX`) |
| config.timeout | string | `"10s"` | HTTP request timeout (env: `CT_TIMEOUT`) |
| config.timeout_shutdown | string | `"10s"` | Timeout to wait on shutdown to allow load balancers detect that we're going away. During this period after the shutdown command the /alive endpoint will reply with HTTP 503. Set to 0s to disable. (env: `CT_TIMEOUT_SHUTDOWN`) |
| envs | list | `[]` | Additional environment variables |
| fullnameOverride | string | `nil` | Application fullname override |
| image.pullPolicy | string | `"IfNotPresent"` | Policy when pulling images |
| image.repository | string | `"ghcr.io/blind-oracle/cortex-tenant"` | Repository to pull the image |
| image.tag | string | `""` | Overrides the image tag (default is `.Chart.appVersion`) |
| nameOverride | string | `nil` | Application name override |
| nodeSelector | object | `{}` | [Node Selection](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node) |
| podAnnotations | object | `{}` | Annotations for pods |
| podDisruptionBudget.enabled | bool | `true` | If enabled, PodDisruptionBudget resources are created |
| podDisruptionBudget.minAvailable | int | `1` | Minimum number of pods that must remain scheduled |
| podSecurityContext | object | `{}` | [Security Context](https://kubernetes.io/docs/tasks/configure-pod-container/security-context) |
| podTopologySpreadConstraints | list | `[]` | [Pod Topology Spread Constraints](https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/) |
| replicas | int | `1` | Number of replicas. Ignored if `autoscaling.enabled` is true |
| resources.limits | object | `{"memory":"256Mi"}` | Resources limits |
| resources.requests | object | `{"cpu":"100m","memory":"128Mi"}` | Resources requests |
| securityContext | object | `{}` | [Security Context](https://kubernetes.io/docs/tasks/configure-pod-container/security-context) |
| service.port | int | `8080` | The port on which the service listens for traffic |
| service.targetPort | int | `8080` |  |
| service.type | string | `"ClusterIP"` | The type of service |
| serviceMonitor.annotations | object | `{}` | ServiceMonitor annotations |
| serviceMonitor.enabled | bool | `false` | If enabled, ServiceMonitor resources for Prometheus Operator are created |
| serviceMonitor.interval | string | `nil` | ServiceMonitor scrape interval |
| serviceMonitor.labels | object | `{}` | Additional ServiceMonitor labels |
| serviceMonitor.metricRelabelings | list | `[]` | ServiceMonitor relabel configs to apply to samples as the last step before ingestion https://github.com/prometheus-operator/prometheus-operator/blob/master/Documentation/api.md#relabelconfig (defines `metric_relabel_configs`) |
| serviceMonitor.namespace | string | `nil` | Alternative namespace for ServiceMonitor resources |
| serviceMonitor.namespaceSelector | object | `{}` | Namespace selector for ServiceMonitor resources |
| serviceMonitor.prometheusRule | object | `{"additionalLabels":{},"enabled":false,"rules":[]}` | Prometheus rules will be deployed for alerting purposes |
| serviceMonitor.relabelings | list | `[]` | ServiceMonitor relabel configs to apply to samples before scraping https://github.com/prometheus-operator/prometheus-operator/blob/master/Documentation/api.md#relabelconfig (defines `relabel_configs`) |
| serviceMonitor.scheme | string | `"http"` | ServiceMonitor will use http by default, but you can pick https as well |
| serviceMonitor.scrapeTimeout | string | `nil` | ServiceMonitor scrape timeout in Go duration format (e.g. 15s) |
| serviceMonitor.targetLabels | list | `[]` | ServiceMonitor will add labels from the service to the Prometheus metric https://github.com/prometheus-operator/prometheus-operator/blob/main/Documentation/api.md#servicemonitorspec |
| serviceMonitor.targetPort | int | `9090` |  |
| serviceMonitor.tlsConfig | string | `nil` | ServiceMonitor will use these tlsConfig settings to make the health check requests |
| tolerations | list | `[]` | [Taints and Tolerations](https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/) |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.11.3](https://github.com/norwoodj/helm-docs/releases/v1.11.3)
