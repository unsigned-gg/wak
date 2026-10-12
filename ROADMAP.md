# wak — System Roadmap

> Accept-and-hold TCP wake proxy + idle reaper: scale-to-zero activator for Kubernetes StatefulSets and Deployments.
> Repo: `unsigned-gg/wak` · Binary: `waker` · Codename: portolan · LeanSpec: [SPEC-0001](specs/SPEC-0001-waker-architecture-and-roadmap.md)

---

## 1. Operational State (October 2026)

- **v1.1.0+** deployed and operating in production cluster `Cygnus`.
- Dedicated TCP listener per configured target with bidirectional raw byte splicing.
- Scale-to-zero activator supporting both `StatefulSet` and `Deployment` via the `/scale` subresource.
- Periodic in-memory idle reaper with opt-in annotation gates (`unsigned.gg/wake-enabled: "true"`).
- OTLP distributed tracing exported to `alloy-gateway.monitoring.svc:4317` (`waker.session`, `waker.ensure_up`, `waker.reap`).
- Zero third-party runtime dependencies; minimal stdlib Kubernetes REST client strictly matched to RBAC Roles.
- CI pipeline enforced: `gofmt`, ThreadSanitizer race detection, static binary linkage verification, CodeQL, and Helm lint.

---

## 2. Strategic Integration Vectors

```
                                  ┌───────────────────────────────┐
                                  │      escapement (EST-34)      │
                                  │   wake_and_wait task dispatch │
                                  └──────────────┬────────────────┘
                                                 │
                                                 ▼
┌───────────────────────────────┐         ┌─────────────┐         ┌───────────────────────────────┐
│     soma Function Agents      │────────►│     wak     │◄────────│      Firecracker / fcsm       │
│  FunctionAgent.spec.wake      │         │   (waker)   │         │  Snapshot/resume microVMs     │
│  (EST-205 contract)           │         └──────┬──────┘         │  (OPS-673 sub-second wake)    │
└───────────────────────────────┘                │                └───────────────────────────────┘
                                                 ▼
                                  ┌───────────────────────────────┐
                                  │      Kubernetes Workloads     │
                                  │   StatefulSets & Deployments  │
                                  └───────────────────────────────┘
```

---

## 3. Horizon Milestones

### Horizon 1: Generic Scale-to-Zero Activator (Status: Completed)
- [x] Dual-workload accept-and-hold proxy (`StatefulSet` and `Deployment`).
- [x] Non-destructive raw TCP byte splicing (zero crypto termination).
- [x] In-memory idle reaper with annotation opt-in and per-workload timeouts.
- [x] OTLP distributed tracing across connection lifecycle and reconciliation loops.
- [x] Hardened CI pipeline: format check, `-race` execution, static ELF assertion.

### Horizon 2: Escapement & Function-Agent Contract (Status: Planned)
- **Escapement Integration ([EST-34](https://linear.app/cerebral-work/issue/EST-34)):** Provide deterministic `wake_and_wait(target) -> Result<Ready>` flow for autonomous job dispatch.
- **Protocol Semantics Alignment ([EST-78](https://linear.app/cerebral-work/issue/EST-78)):** Align task dispatcher client timeouts with waker's accept-and-hold connection buffering, avoiding premature dropouts before pod ready.
- **Function-Agent Contract ([EST-205](https://linear.app/cerebral-work/issue/EST-205)):** Specify and implement declarative workload registration (`FunctionAgent.spec.wake`), supporting zero-ingress originating workloads and RWO volume attachment constraints.

### Horizon 3: Observability & In-Guest Probing (Status: Planned)
- **Prometheus Metrics Exporter ([OPS-672](https://linear.app/cerebral-work/issue/OPS-672)):** Expose `/metrics` endpoint with latency histograms, active spliced stream gauges, reap execution counters, and saved core-hour calculations.
- **In-Guest Activity Probes ([OPS-672](https://linear.app/cerebral-work/issue/OPS-672)):** Interrogate in-guest agents or apiserver audit logs to detect active background computation, preventing premature idle reaping during non-interactive tasks.
- **High-Availability Data Path:** Evaluate hot-standby leader election to prevent connection interruption during waker rolling updates.

### Horizon 4: Pluggable Backends & Transport Adaptation (Status: Planned)
- **`fcsm` Snapshot/Resume Engine ([OPS-673](https://linear.app/cerebral-work/issue/OPS-673)):** Abstract backend lifecycle behind `WakeBackend` interface. Support Firecracker microVM snapshot/resume via the `fcsm` session API for sub-second wake times.
- **Kata Containers Transport Adapter ([OPS-1109](https://linear.app/cerebral-work/issue/OPS-1109)):** Implement proxy adaptation for `kata-clh` workloads fronted by `kubectl port-forward` rather than ClusterIP Services.

### Horizon 5: Multi-Cluster Federation & Predictive Health Gating (Status: Backlog)
- **Predictive Health Gating:** Inspect workload restart telemetry and terminate connection wait early if the target pod enters `CrashLoopBackOff` or `ErrImagePull`.
- **Multi-Cluster Federation:** Synchronize scale-to-zero proxies across hybrid environments (Cygnus bare metal and Orion cloud).
