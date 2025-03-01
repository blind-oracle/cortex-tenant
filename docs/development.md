# Development

Getting started locally is pretty easy. You can execute:

```shell
make e2e-build
```

This installs all required operators an installs the operator within a [KinD Cluster](https://kind.sigs.k8s.io/). The required binaries are also downloaded.

If you wish to test against a specific Kubernetes version, you can pass that via variable:

```shell
KIND_K8S_VERSION="v1.31.0" make e2e-build
```

When you want to quickly develop, you can scale down the operator within the cluster:

```shell
kubectl scale deploy capsule-argo-addon --replicas=0 -n capsule-argo-addon
```

And then execute the binary:

```shell
go run cmd/main.go -zap-log-level=10
```

You might need to first export the Kubeconfig for the cluster (If you are using multiple clusters at the same time):

```shell
bin/kind get kubeconfig --name capsule-arg-addon  > /tmp/capsule-argo-addon
export KUBECONFIG="/tmp/capsule-argo-addon"
```

## Testing

When you are done with the development run the following commands.

For Liniting

```shell
make golint
```

For Unit-Testing

```shell
make test
```

For Unit-Testing (When running Unit-Tests there should not be any `argotranslators`, `tenants` and `appprojects` present):

```shell
make e2e-exec
```

## Helm Chart

When making changes to the Helm-Chart, Update the documentation by running:

```shell
make helm-docs
```

Linting and Testing the chart:

```shell
make helm-lint
make helm-test
```

## Performance

Use [PProf](https://book.kubebuilder.io/reference/pprof-tutorial) for profiling:

```shell
curl -s "http://127.0.0.1:8082/debug/pprof/profile" > ./cpu-profile.out

go tool pprof -http=:8080 ./cpu-profile.out
```
