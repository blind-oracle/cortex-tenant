# Capsule ❤️ Cortex

![Logo](https://github.com/projectcapsule/cortex-tenant/blob/main/docs/images/logo.png)

## Installation

1. Install Helm Chart:

        $ helm install cortex-tenant oci://ghcr.io/projectcapsule/charts/cortex-tenant  -n monitioring-system

3. Show the status:

        $ helm status cortex-tenant -n monitioring-system

4. Upgrade the Chart

        $ helm upgrade cortex-tenant oci://ghcr.io/projectcapsule/charts/cortex-tenant --version 0.4.7

5. Uninstall the Chart

        $ helm uninstall cortex-tenant -n monitioring-system

## Values

The following Values are available for this chart.

### Global Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|

### General Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Set affinity rules |
| args.extraArgs | list | `[]` | A list of extra arguments to add to the capsule-argo-addon |
| args.logLevel | int | `4` | Log Level |
| args.pprof | bool | `false` | Enable Profiling |
| fullnameOverride | string | `""` |  |
| image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy. |
| image.registry | string | `"ghcr.io"` | Set the image registry |
| image.repository | string | `"projectcapsule/cortex-tenant"` | Set the image repository |
| image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion. |
| imagePullSecrets | list | `[]` | Configuration for `imagePullSecrets` so that you can use a private images registry. |
| livenessProbe | object | `{"httpGet":{"path":"/healthz","port":10080}}` | Configure the liveness probe using Deployment probe spec |
| nameOverride | string | `""` |  |
| nodeSelector | object | `{}` | Set the node selector |
| pdb.enabled | bool | `false` | Specifies whether an hpa should be created. |
| pdb.minAvailable | int | `1` | The number of pods from that set that must still be available after the eviction |
| podAnnotations | object | `{}` | Annotations to add |
| podSecurityContext | object | `{"seccompProfile":{"type":"RuntimeDefault"}}` | Set the securityContext |
| priorityClassName | string | `""` | Set the priority class name of the Capsule pod |
| rbac.enabled | bool | `true` | Enable bootstraping of RBAC resources |
| readinessProbe | object | `{"httpGet":{"path":"/readyz","port":10080}}` | Configure the readiness probe using Deployment probe spec |
| replicaCount | int | `1` | Amount of replicas |
| resources | object | `{"limits":{"cpu":"200m","memory":"128Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}` | Set the resource requests/limits |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsNonRoot":true,"runAsUser":1000}` | Set the securityContext for the container |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account. |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| serviceAccount.name | string | `""` | The name of the service account to use. |
| tolerations | list | `[]` | Set list of tolerations |
| topologySpreadConstraints | list | `[]` | Set topology spread constraints |

### Config Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| config.backend.auth.password | string | `""` | Password |
| config.backend.auth.username | string | `""` | Username |
| config.backend.url | string | `"http://cortex-distributor.cortex.svc:8080/api/v1/push"` | Where to send the modified requests (Cortex) |
| config.concurrency | int | `1000` | Max number of parallel incoming HTTP requests to handle |
| config.ipv6 | bool | `false` | Whether to enable querying for IPv6 records |
| config.maxConnectionDuration | string | `"0s"` | Maximum duration to keep outgoing connections alive (to Cortex/Mimir) Useful for resetting L4 load-balancer state Use 0 to keep them indefinitely |
| config.maxConnectionsPerHost | int | `64` | This parameter sets the limit for the count of outgoing concurrent connections to Cortex / Mimir. By default it's 64 and if all of these connections are busy you will get errors when pushing from Prometheus. If your `target` is a DNS name that resolves to several IPs then this will be a per-IP limit. |
| config.metadata | bool | `false` | Whether to forward metrics metadata from Prometheus to Cortex Since metadata requests have no timeseries in them - we cannot divide them into tenants So the metadata requests will be sent to the default tenant only, if one is not defined - they will be dropped |
| config.tenant.acceptAll | bool | `false` | Enable if you want all metrics from Prometheus to be accepted with a 204 HTTP code regardless of the response from Cortex. This can lose metrics if Cortex is throwing rejections. |
| config.tenant.default | string | `"cortex-tenant-default"` | Which tenant ID to use if the label is missing in any of the timeseries If this is not set or empty then the write request with missing tenant label will be rejected with HTTP code 400 |
| config.tenant.header | string | `"X-Scope-OrgID"` | To which header to add the tenant ID |
| config.tenant.labelRemove | bool | `false` | Whether to remove the tenant label from the request |
| config.tenant.labels | list | `[]` | List of labels examined for tenant information. If set takes precedent over `label` |
| config.tenant.prefix | string | `""` | Optional hard-coded prefix with delimeter for all tenant values. Delimeters allowed for use: https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/ |
| config.tenant.prefixPreferSource | bool | `false` | If true will use the tenant ID of the inbound request as the prefix of the new tenant id. Will be automatically suffixed with a `-` character. Example:   Prometheus forwards metrics with `X-Scope-OrgID: Prom-A` set in the inbound request.   This would result in the tenant prefix being set to `Prom-A-`. |
| config.timeout | string | `"10s"` | HTTP request timeout |
| config.timeoutShutdown | string | `"10s"` | Timeout to wait on shutdown to allow load balancers detect that we're going away. During this period after the shutdown command the /alive endpoint will reply with HTTP 503. Set to 0s to disable. |

### Autoscaling Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| autoscaling.annotations | object | `{}` | Annotations to add to the hpa. |
| autoscaling.behavior | object | `{}` | HPA [behavior](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/) |
| autoscaling.enabled | bool | `false` | Specifies whether an hpa should be created. |
| autoscaling.labels | object | `{}` | Labels to add to the hpa. |
| autoscaling.maxReplicas | int | `3` | Set the maxReplicas for hpa. |
| autoscaling.metrics | list | `[]` | Custom [metrics-objects](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale-walkthrough/#autoscaling-on-multiple-metrics-and-custom-metrics) for capsule-proxy hpa |
| autoscaling.minReplicas | int | `1` | Set the minReplicas for hpa. |
| autoscaling.targetCPUUtilizationPercentage | int | `0` | Set the targetCPUUtilizationPercentage for hpa. |
| autoscaling.targetMemoryUtilizationPercentage | int | `0` | Set the targetMemoryUtilizationPercentage for hpa. |

### Monitoring Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| monitoring.enabled | bool | `false` | Enable Monitoring of the Operator |
| monitoring.rules.annotations | object | `{}` | Assign additional Annotations |
| monitoring.rules.enabled | bool | `true` | Enable deployment of PrometheusRules |
| monitoring.rules.groups | list | `[{"name":"TranslatorAlerts","rules":[{"alert":"TranslatorNotReady","annotations":{"description":"The Translator {{ $labels.name }} has been in a NotReady state for over 5 minutes.","summary":"Translator {{ $labels.name }} is not ready"},"expr":"cca_translator_condition{status=\"NotReady\"} == 1","for":"5m","labels":{"severity":"warning"}}]}]` | Prometheus Groups for the rule |
| monitoring.rules.labels | object | `{}` | Assign additional labels |
| monitoring.rules.namespace | string | `""` | Install the rules into a different Namespace, as the monitoring stack one (default: the release one) |
| monitoring.serviceMonitor.annotations | object | `{}` | Assign additional Annotations |
| monitoring.serviceMonitor.enabled | bool | `true` | Enable ServiceMonitor |
| monitoring.serviceMonitor.endpoint.interval | string | `"15s"` | Set the scrape interval for the endpoint of the serviceMonitor |
| monitoring.serviceMonitor.endpoint.metricRelabelings | list | `[]` | Set metricRelabelings for the endpoint of the serviceMonitor |
| monitoring.serviceMonitor.endpoint.relabelings | list | `[]` | Set relabelings for the endpoint of the serviceMonitor |
| monitoring.serviceMonitor.endpoint.scrapeTimeout | string | `""` | Set the scrape timeout for the endpoint of the serviceMonitor |
| monitoring.serviceMonitor.jobLabel | string | `"app.kubernetes.io/name"` | Prometheus Joblabel |
| monitoring.serviceMonitor.labels | object | `{}` | Assign additional labels according to Prometheus' serviceMonitorSelector matching labels |
| monitoring.serviceMonitor.matchLabels | object | `{}` | Change matching labels |
| monitoring.serviceMonitor.namespace | string | `""` | Install the ServiceMonitor into a different Namespace, as the monitoring stack one (default: the release one) |
| monitoring.serviceMonitor.serviceAccount.name | string | `""` |  |
| monitoring.serviceMonitor.serviceAccount.namespace | string | `""` |  |
| monitoring.serviceMonitor.targetLabels | list | `[]` | Set targetLabels for the serviceMonitor |
