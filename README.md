# Polars Kubernetes Operator

Kubernetes operator for managing Polars Clusters

## Description
// TODO(user): An in-depth paragraph about your project and overview of use

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/polars-k8s-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/polars-k8s-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Releases are cut by pushing a semver tag (`vX.Y.Z`), which triggers the `release`
workflow:

- `docker` pushes the multi-arch image to [`polarscloud/polars-k8s-operator`](https://hub.docker.com/r/polarscloud/polars-k8s-operator) on Docker Hub.
- `manifests` (after `docker`) creates a GitHub release with generated notes and the installer bundle attached.
- `chart` (after `docker`) opens a PR against [polars-inc/helm-charts](https://github.com/polars-inc/helm-charts) with the regenerated chart; merging it publishes the chart.

The workflow can also be run manually via `workflow_dispatch` (with a dry-run
option: no image push, draft release, chart diff logged instead of a PR).

### Installing from the YAML bundle

```sh
kubectl apply -f https://github.com/polars-inc/polars-k8s-operator/releases/download/vX.Y.Z/install.yaml
```

### Installing from the Helm chart

```sh
helm repo add polars-inc https://polars-inc.github.io/helm-charts
helm install polars-k8s-operator polars-inc/polars-k8s-operator
```

**NOTE:** The chart and installer bundle are generated from `config/` (`make
helm-generate` and `make build-installer`); `dist/` is not tracked in git.
Durable chart changes must be made in `config/`, not in `dist/chart`.

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

