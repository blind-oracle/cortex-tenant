# cortex-tenant

![Version: 0.2.0](https://img.shields.io/badge/Version-0.2.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 1.11.0](https://img.shields.io/badge/AppVersion-1.11.0-informational?style=flat-square)

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
| tolerations | list | `[]` |  |
