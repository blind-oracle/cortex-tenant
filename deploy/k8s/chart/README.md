# cortex-tenant

![Version: 0.3.0](https://img.shields.io/badge/Version-0.3.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 1.11.0](https://img.shields.io/badge/AppVersion-1.11.0-informational?style=flat-square)

A Helm Chart for cortex-tenant

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| autoscaling.enabled | bool | `true` |  |
| autoscaling.maxReplica | int | `3` |  |
| autoscaling.minReplica | int | `1` |  |
| autoscaling.targetCPUUtilizationPercentage | int | `50` |  |
| autoscaling.targetMemoryAverageValue | string | `"100Mi"` |  |
| config.auth.enabled | bool | `false` |  |
| config.auth.existingSecret | string | `nil` |  |
| config.auth.password | string | `nil` |  |
| config.auth.username | string | `nil` |  |
| config.concurrency | int | `1000` |  |
| config.enable_ipv6 | bool | `false` |  |
| config.listen | string | `"0.0.0.0:8080"` |  |
| config.listen_pprof | string | `"0.0.0.0:7008"` |  |
| config.log_level | string | `"warn"` |  |
| config.log_response_errors | bool | `true` |  |
| config.max_connection_duration | string | `"0s"` |  |
| config.metadata | bool | `false` |  |
| config.target | string | `"http://cortex-distributor.cortex.svc:8080/api/v1/push"` |  |
| config.tenant.accept_all | bool | `false` |  |
| config.tenant.default | string | `"cortex-tenant-default"` |  |
| config.tenant.header | string | `"X-Scope-OrgID"` |  |
| config.tenant.label | string | `"tenant"` |  |
| config.tenant.label_remove | bool | `false` |  |
| config.tenant.prefix | string | `""` |  |
| config.timeout | string | `"10s"` |  |
| config.timeout_shutdown | string | `"10s"` |  |
| envs | string | `nil` |  |
| fullnameOverride | string | `nil` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ghcr.io/blind-oracle/cortex-tenant"` |  |
| image.tag | string | `""` |  |
| nameOverride | string | `nil` |  |
| nodeSelector | object | `{}` |  |
| podAnnotations | object | `{}` |  |
| podDisruptionBudget.enabled | bool | `true` |  |
| podDisruptionBudget.minAvailable | int | `1` |  |
| podSecurityContext.fsGroup | int | `1000` |  |
| podSecurityContext.runAsGroup | int | `1000` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `1000` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"100m"` |  |
| resources.requests.memory | string | `"128Mi"` |  |
| securityContext | object | `{}` |  |
| service.port | int | `8080` |  |
| service.targetPort | int | `8080` |  |
| service.type | string | `"ClusterIP"` |  |
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
| serviceMonitor.tlsConfig | string | `nil` | ServiceMonitor will use these tlsConfig settings to make the health check requests |
| tolerations | list | `[]` |  |
