# kube-dethrottler

The goal of `kube-dethrottler` is to prevent Kubernetes nodes from becoming overloaded by actively monitoring system load averages. While the default Kubernetes scheduler considers CPU and memory requests, it doesn't inherently react to overall system load, which can be influenced by factors like intense I/O operations not fully captured by CPU metrics alone. High system load can lead to nodes becoming unresponsive or performing poorly, even if CPU utilization isn't at its absolute peak.

`kube-dethrottler` addresses this by monitoring normalized load averages and applying a Kubernetes taint to overloaded nodes. This action discourages the scheduler from placing new pods on them until the load subsides and the node stabilizes.

## Features

- Reads 1-minute, 5-minute, and 15-minute load averages from `/proc/loadavg`.
- Normalizes load averages by the number of CPU cores on the node.
- Compares normalized load averages against configurable thresholds.
- Applies a user-defined taint (key, value, effect) to the node if any threshold is exceeded.
- Removes the taint if all load metrics fall below their thresholds and a configurable cooldown period has passed.
- All configurations (polling interval, cooldown, thresholds, taint details) are managed via a YAML file, typically mounted as a ConfigMap.
- Runs as a DaemonSet, ensuring it operates on each (or selected) nodes in the cluster.
- Uses the Downward API to automatically determine the node name it's running on.
- Graceful shutdown: attempts to remove the applied taint on termination (SIGINT, SIGTERM).

## How it Works

1.  **Configuration**: `kube-dethrottler` loads its settings from a YAML file specified by the `--config` flag (default: `/etc/kube-dethrottler/config.yaml`).
2.  **Node Identification**: It automatically identifies the node it's running on using the `NODE_NAME` environment variable, typically injected via the Kubernetes Downward API.
3.  **Load Monitoring**: At a `pollInterval` (e.g., every 10 seconds):
    *   It reads the raw load averages (1m, 5m, 15m) from `/proc/loadavg`.
    *   It fetches the number of CPU cores on the node.
    *   It normalizes the raw load averages by dividing them by the CPU core count. This provides a load-per-core metric.
4.  **Threshold Checking**: The normalized load averages are compared against `thresholds` defined in the configuration.
    *   Each threshold ( `load1m`, `load5m`, `load15m`) can be set independently. A value of `0` for a threshold disables the check for that specific period.
5.  **Tainting Logic**:
    *   **Apply Taint**: If any of the active (non-zero) thresholds are exceeded by their corresponding normalized load average, and the node is not already tainted by this controller:
        *   `kube-dethrottler` applies a taint to the node. The configurable components are the taint `key` (e.g., `kube-dethrottler/high-load`) and `effect` (e.g., `NoSchedule` and `PreferNoSchedule`). The `taintValue` is consistently applied by the controller as `high-load`.
        *   The time of tainting is recorded.
    *   **Maintain Taint**: If thresholds are still exceeded and the node is already tainted, the `lastTaintTime` is updated to effectively reset/extend the cooldown consideration period if the load remains high.
    *   **Remove Taint**: If all normalized load averages are *below* their respective thresholds:
        *   The controller checks if a `cooldownPeriod` (e.g., 5 minutes) has passed since the taint was last applied or updated.
        *   If the cooldown period has elapsed and the node is currently tainted by this controller, the taint is removed.
6.  **Initial State**: Upon startup, `kube-dethrottler` checks if its managed taint already exists on the node. If so, it assumes it was previously tainted (e.g., before a restart) and initializes its state accordingly, including setting `lastTaintTime` for cooldown calculations.
7.  **Graceful Shutdown**: When receiving a SIGINT or SIGTERM signal, `kube-dethrottler` will attempt to remove its managed taint from the node before exiting.

## Configuration File Example

A typical configuration file (`config.yaml`) would look like this:

```yaml
# nodeName: "my-node-override" # Optional: Typically injected by Downward API (NODE_NAME env var). Used for local testing if needed.

pollInterval: "10s" # How often to check load averages. Recommended: 10s-30s.
cooldownPeriod: "5m" # How long to wait after load returns to normal before removing the taint. Recommended: 5m-15m.

taintKey: "kube-dethrottler/high-load" # Taint key to apply.
taintEffect: "NoSchedule" # Taint effect. Options: NoSchedule, PreferNoSchedule, NoExecute.

# Thresholds for *normalized* load averages (raw load / CPU cores).
# A value of 0 for any threshold disables the check for that specific period.
thresholds:
  load1m: 2.0  # Taint if 1-minute load avg per CPU core exceeds 2.0
  load5m: 1.5  # Taint if 5-minute load avg per CPU core exceeds 1.5
  load15m: 1.0 # Taint if 15-minute load avg per CPU core exceeds 1.0

# kubeconfigPath: "/path/to/local/kubeconfig" # Optional: Path to kubeconfig for out-of-cluster development. Leave empty for in-cluster.
```

**Important Notes on Thresholds:**

*   The `thresholds` are for **normalized load averages**. This means the raw load average value from `/proc/loadavg` is divided by the number of CPU cores before comparison.
*   For example, on a 4-core node, a `thresholds.load1m: 2.0` means the node will be considered for tainting if its raw 1-minute load average reaches `2.0 * 4 = 8.0`.
*   Setting a threshold to `0` effectively disables the check for that particular load average period (1m, 5m, or 15m).
*   At least one threshold must be set to a nonzero value.

## Helm Chart Installation

`kube-dethrottler` is deployed as a DaemonSet using the provided Helm chart.

1.  **Add Helm Repository (if applicable, or use local chart path)**:
    ```bash
    # helm repo add <your-repo-name> <your-repo-url>
    # helm repo update
    ```

2.  **Install the Chart**:

    To install the chart with the release name `kube-dethrottler` into the `kube-system` namespace (recommended for system-level components):

    Using local chart directory:
    ```bash
    helm install kube-dethrottler ./charts/kube-dethrottler --namespace kube-system --create-namespace
    ```

    If using a Helm repository:
    ```bash
    # helm install kube-dethrottler <your-repo-name>/kube-dethrottler --namespace kube-system --create-namespace
    ```

3.  **Configuration via Values**:

    You can customize the installation by providing a custom `values.yaml` file or by using `--set` arguments during installation.

    Example using `--set`:
    ```bash
    helm install kube-dethrottler ./charts/kube-dethrottler \
      --namespace kube-system \
      --create-namespace \
      --set config.pollInterval="15s" \
      --set config.thresholds.load1m=2.5 \
      --set config.thresholds.load5m=2.0
    ```

    Refer to the `charts/kube-dethrottler/values.yaml` file for all configurable parameters.

## Building from Source

This section is intended for developers who wish to contribute to `kube-dethrottler`, create custom builds, or understand the build process.

Prerequisites:
* Go (version specified in `go.mod`)
* Docker (for building images)
* Make
* GolangCI-Lint (for linting)

Key Makefile targets:
* `make build`: Builds the Go binary.
* `make test`: Runs unit tests.
* `make lint`: Runs linters.
* `make docker-build`: Builds the Docker image.
* `make docker-push`: Pushes the Docker image (ensure `IMAGE_NAME` and `IMAGE_TAG` are set).
* `make helm-install`: Installs the Helm chart from the local `./charts` directory.

## License

Apache License 2.0 