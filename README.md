# Polars Kubernetes Operator

Kubernetes operator for managing Polars Clusters

## Description

This operator manages `PolarsCluster` custom resources, each running a
[Polars On-Premises](https://docs.pola.rs/polars-on-premises/) cluster
(scheduler + worker pool) on Kubernetes. It composes the scheduler/worker
Deployments, Services, licensing, and storage config from a single CR.

See `config/samples/` for runnable examples, and [docs/api.md](docs/api.md) for the full field reference.

## Installation

Both methods below use the prebuilt image at
[`polarscloud/polars-k8s-operator`](https://hub.docker.com/r/polarscloud/polars-k8s-operator)
on Docker Hub. No build required.

### Using the Helm chart (recommended)

```sh
helm repo add polars-inc https://polars-inc.github.io/helm-charts
helm install polars-k8s-operator polars-inc/polars-k8s-operator
```

### Using the YAML bundle

```sh
kubectl apply -f https://github.com/polars-inc/polars-k8s-operator/releases/download/vX.Y.Z/install.yaml
```

### Create a PolarsCluster

Apply the quickstart sample (On-Prem Enterprise license):

```sh
kubectl apply -k config/samples/
```

Other samples in `config/samples/` are standalone. Apply one at a time; each
creates its own scheduler + worker pool:

```sh
kubectl apply -f config/samples/<file>.yaml
```

## API Reference

Generated reference documentation for the `PolarsCluster` custom resource lives in
[docs/api.md](docs/api.md). It is generated from the Go types in `api/v1alpha1`.
After changing them, run `make api-docs` and commit the result (CI fails on stale docs).

## Development

Building and running the operator from source, for working on the operator itself.

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster

Build and push your image:

```sh
make docker-build docker-push IMG=<some-registry>/polars-k8s-operator:tag
```

Install the CRDs:

```sh
make install
```

Deploy the manager with the image you pushed:

```sh
make deploy IMG=<some-registry>/polars-k8s-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

### To Uninstall

Delete the instances (CRs):

```sh
kubectl delete -k config/samples/
```

Delete the CRDs:

```sh
make uninstall
```

Undeploy the controller:

```sh
make undeploy
```

## Project Distribution

Releases are cut by pushing a semver tag (`vX.Y.Z`), which triggers the `release`
workflow:

- `docker` pushes the multi-arch image to [`polarscloud/polars-k8s-operator`](https://hub.docker.com/r/polarscloud/polars-k8s-operator) on Docker Hub.
- `manifests` (after `docker`) creates a GitHub release with generated notes and the installer bundle attached (used by the YAML bundle install above).
- `chart` (after `docker`) opens a PR against [polars-inc/helm-charts](https://github.com/polars-inc/helm-charts) with the regenerated chart; merging it publishes the chart used by the Helm install above.

The workflow can also be run manually via `workflow_dispatch`, with a dry-run
option that skips the image push, drafts the release, and logs the chart diff
instead of opening a PR.

**NOTE:** The chart and installer bundle are generated from `config/` (`make
helm-generate` and `make build-installer`). `dist/` is not tracked in git.
Durable chart changes must be made in `config/`, not in `dist/chart`.

## License

MIT. See [LICENSE](LICENSE).
