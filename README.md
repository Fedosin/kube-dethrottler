# kube-dethrottler

`kube-dethrottler` prevents Kubernetes nodes from becoming overloaded by monitoring [Pressure Stall Information (PSI)](https://kubernetes.io/docs/reference/instrumentation/understand-psi-metrics/) metrics and applying taints to nodes under pressure. This discourages the scheduler from placing new pods on stressed nodes until conditions improve.

## Features

- Monitors PSI metrics (CPU, memory, I/O) for all nodes via the kubelet Summary API.
- Supports both "some" and "full" pressure categories with `avg10`, `avg60`, and `avg300` windows.
- Applies a configurable taint to nodes when any threshold is exceeded.
- Removes the taint after all metrics fall below thresholds and a cooldown period has passed.
- Runs as a centralized Deployment (no per-node DaemonSet needed).
- Supports leader election for high-availability deployments.
- Filters nodes by label selector to target specific node groups.
- Graceful shutdown: removes all applied taints on termination.

## Requirements

- **Kubernetes >= 1.36** (PSI metrics stable, `KubeletPSI` feature gate locked to true)
- **cgroup v2** on all monitored nodes
- **Linux kernel >= 4.20** with `CONFIG_PSI=y` (most modern distributions enable this by default)

## How it Works

1. **Configuration**: Loads settings from a YAML file specified by `--config` (default: `/etc/kube-dethrottler/config.yaml`).
2. **Node Discovery**: Lists all nodes matching the configured `nodeFilter` label selector (or all nodes if empty).
3. **PSI Polling**: At each `pollInterval`, queries the kubelet Summary API (`/api/v1/nodes/<name>/proxy/stats/summary`) for every monitored node to retrieve node-level PSI data.
4. **Threshold Checking**: Compares PSI values (cpu/memory/io, some/full, avg10/avg60/avg300) against configured thresholds. A threshold of `0` disables that check.
5. **Tainting Logic**:
   - **Apply Taint**: If any enabled threshold is exceeded and the node is not already tainted, applies the configured taint.
   - **Extend Cooldown**: If thresholds remain exceeded on an already-tainted node, the cooldown timer resets.
   - **Remove Taint**: If all metrics are below thresholds and the cooldown period has elapsed, removes the taint.
6. **Leader Election**: When enabled, only the leader instance actively polls and taints. Standby replicas wait to acquire leadership.
7. **Graceful Shutdown**: On SIGINT/SIGTERM, removes all taints applied by this instance.

## Configuration File Example

```yaml
pollInterval: "30s"
cooldownPeriod: "5m"

taintKey: "kube-dethrottler/high-load"
taintEffect: "NoSchedule"

# Only monitor worker nodes (empty = all nodes)
nodeFilter: "node-role.kubernetes.io/worker"

leaderElection:
  enabled: true
  leaseName: "kube-dethrottler-leader"
  leaseNamespace: "kube-system"

# PSI pressure thresholds (percentage, 0-100). 0 disables the check.
thresholds:
  cpu:
    some:
      avg10: 25.0
      avg60: 0
      avg300: 0
    full:
      avg10: 10.0
      avg60: 0
      avg300: 0
  memory:
    some:
      avg10: 20.0
      avg60: 0
      avg300: 0
    full:
      avg10: 5.0
      avg60: 0
      avg300: 0
  io:
    some:
      avg10: 30.0
      avg60: 0
      avg300: 0
    full:
      avg10: 15.0
      avg60: 0
      avg300: 0
```

**Understanding PSI Thresholds:**

- PSI values represent the percentage of wall-clock time that tasks were stalled on a resource.
- `some`: at least one task is stalled (early indicator of contention).
- `full`: all non-idle tasks are stalled simultaneously (severe shortage).
- `avg10`/`avg60`/`avg300`: 10-second, 60-second, and 5-minute moving averages respectively.
- Example: `cpu.some.avg10: 25.0` means taint the node if at least one task was CPU-stalled for more than 25% of the last 10 seconds.

## Helm Chart Installation

`kube-dethrottler` is deployed as a Deployment using the provided Helm chart.

Install the chart with the release name `kube-dethrottler` into the `kube-system` namespace:

```bash
helm install kube-dethrottler ./charts/kube-dethrottler --namespace kube-system --create-namespace
```

Customize the installation with `--set` arguments:

```bash
helm install kube-dethrottler ./charts/kube-dethrottler \
  --namespace kube-system \
  --create-namespace \
  --set config.pollInterval="15s" \
  --set config.thresholds.cpu.some.avg10=30 \
  --set config.thresholds.memory.full.avg10=10
```

Refer to `charts/kube-dethrottler/values.yaml` for all configurable parameters.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  kube-dethrottler Deployment (with leader election)         │
│                                                             │
│  ┌──────────────────────────────────────────────┐           │
│  │  Control Loop (runs on leader only)          │           │
│  │                                              │           │
│  │  1. List nodes (with label filter)           │           │
│  │  2. For each node:                           │           │
│  │     - GET /proxy/stats/summary               │           │
│  │     - Compare PSI values vs thresholds       │           │
│  │     - Apply/remove taint as needed           │           │
│  └──────────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────────┘
           │                              │
           │ PSI metrics via              │ Taint/untaint
           │ kube-apiserver proxy         │ via kube-apiserver
           ▼                              ▼
┌─────────────────┐            ┌─────────────────┐
│  Node 1 kubelet │            │  Node 2 kubelet │
└─────────────────┘            └─────────────────┘
```

## Building from Source

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
* `make docker-push`: Pushes the Docker image.
* `make helm-install`: Installs the Helm chart from the local `./charts` directory.

## License

Apache License 2.0
