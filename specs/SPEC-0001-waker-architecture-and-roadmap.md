# SPEC-0001: Waker Architecture, Invariants & Multi-Horizon Roadmap

## 1. Status & Metadata
- **Status:** Approved & Living
- **Authors:** Heimdall <heimdall@mesh.unsigned.gg>, Mimir <mimir@unsigned.gg>
- **Champion Inspiration:** Joe Armstrong, Carl Lerche, Leslie Lamport
- **Target System:** `wak` (`unsigned-gg/wak`, binary `waker`, chart `charts/waker`)
- **License:** CC0-1.0 Universal / Apache-2.0 OR MIT

---

## 2. Problem Statement
Fixed-capacity Kubernetes clusters running persistent interactive workloads (such as engineer microVMs, headless language runtime engines, and isolated development environments) experience severe resource fragmentation. In multi-tenant environments like `unsigned-paas` on Cygnus, keeping idle pods permanently scheduled wastes CPU, memory, and IP headroom.

Standard Kubernetes scale-to-zero solutions (such as Knative Activator or KEDA) operate primarily at the HTTP/L7 layer and cannot hold raw TCP connections open during cold boot. Furthermore, conventional serverless frameworks require client-side SDK modifications or custom reconnection logic.

`wak` resolves this problem by implementing a transparent **accept-and-hold TCP proxy** combined with an **idle reaper**. By exploiting the pre-banner connection buffering inherent to standard TCP protocols (such as OpenSSH), `wak` accepts incoming client connections, holds them while scaling the target workload from 0 to 1, awaits pod readiness and network reachability, and then transparently splices bytes without terminating cryptography.

---

## 3. Mathematical Foundations & Operational Invariants

### Invariant 1: Cryptographic Envelope Transparency (Raw Byte Splicing)
For any active connection session $S$ with inbound stream $I$ and outbound stream $O$:
$$\forall b \in I, \quad \text{emit}(b, O) \quad \text{without cryptographic inspection or termination}$$
The waker never terminates TLS, SSH, or custom encryption envelopes. All transport-layer crypto remains strictly end-to-end between client and pod backend.

### Invariant 2: Bounded Dial Holding Timeout
Let $t_{\text{accept}}$ be the timestamp when an ingress TCP connection is accepted, and $t_{\text{ready}}$ be the timestamp when the backend TCP dial succeeds:
$$t_{\text{ready}} - t_{\text{accept}} \le T_{\text{cap}} \quad (T_{\text{cap}} = 120\text{ seconds})$$
If $t_{\text{ready}} - t_{\text{accept}} > T_{\text{cap}}$, the waker terminates the client socket with an immediate reset or closure. The target workload remains at $\text{replicas} = 1$ to preserve execution state for operator diagnosis.

### Invariant 3: Zero-Session Monotonic Reaping
A workload $W$ is scaled to zero if and only if all the following conditions hold:
$$\text{Reap}(W) \iff \left( \text{ActiveConnections}(W) = 0 \right) \land \left( t_{\text{now}} - t_{\text{last\_activity}} \ge T_{\text{idle}} \right) \land \left( \text{OptedIn}(W) = \text{true} \right)$$
where $\text{OptedIn}(W)$ is asserted by the annotation `unsigned.gg/wake-enabled: "true"`, and $T_{\text{idle}}$ defaults to 45 minutes unless overridden by `unsigned.gg/idle-timeout`.

### Invariant 4: Crash-Safe Fail-Open Clock Reset
Let $t_{\text{boot}}$ be the restart timestamp of the `waker` daemon:
$$\forall W, \quad t_{\text{last\_activity}}(W) \gets t_{\text{boot}}$$
Idle tracking clocks reside strictly in-memory. Process restarts or node evictions reset all idle timers to zero. The system deliberately fails toward keeping workloads active rather than terminating in-flight or nascent work.

### Invariant 5: Minimal REST RBAC Boundary
The internal Kubernetes REST client uses standard library HTTP primitives exclusively. No heavy external dependencies (such as `client-go`) are permitted. Permitted API operations map 1:1 to declared Kubernetes RBAC Role verbs:
$$\text{Verbs}(W) \subseteq \{\text{GET } \text{workload}, \text{GET } \text{scale}, \text{PATCH } \text{scale}, \text{GET/LIST } \text{pods}\}$$

---

## 4. Architecture & Interface

```
  ┌─────────────────┐
  │   TCP Client    │ (e.g. OpenSSH, database client)
  └────────┬────────┘
           │ TCP Connect
           ▼
  ┌────────────────────────────────────────────────────────┐
  │                   wak (waker binary)                   │
  │                                                        │
  │   ┌────────────────────────────────────────────────┐   │
  │   │          TCP Listeners (Port-per-Target)       │   │
  │   └───────────────────────┬────────────────────────┘   │
  │                           │                            │
  │                           ▼                            │
  │   ┌────────────────────────────────────────────────┐   │
  │   │               Accept & Hold Core               │   │
  │   │   • Read active replica count                  │   │
  │   │   • Hold client in pre-banner wait             │   │
  │   │   • Scale workload 0 -> 1                      │   │
  │   │   • Poll pod Ready + TCP Dial (capped 120s)    │   │
  │   └───────┬────────────────────────────────┬───────┘   │
  │           │                                │           │
  │           ▼                                ▼           │
  │   ┌───────────────┐                ┌───────────────┐   │
  │   │  Splice Loop  │                │  Idle Reaper  │   │
  │   │ (Zero-copy    │                │ (Periodic     │   │
  │   │  io.Copy)     │                │  scale -> 0)  │   │
  │   └───────┬───────┘                └───────┬───────┘   │
  └───────────┼────────────────────────────────┼───────────┘
              │ TCP Dial                       │ PATCH /scale
              ▼                                ▼
  ┌───────────────────────┐        ┌───────────────────────┐
  │     Backend Pod       │        │  kube-apiserver REST  │
  │  (StatefulSet / Dep)  │        │   (In-Cluster SA)     │
  └───────────────────────┘        └───────────────────────┘
```

### Target Configuration Schema
Targets are mounted as JSON via `WAKER_CONFIG` (default `/etc/waker/config.json`):
```json
{
  "idleTimeoutDefault": "45m",
  "targets": [
    {
      "name": "aria",
      "namespace": "aria-vm",
      "kind": "statefulset",
      "workload": "aria-vm",
      "listenPort": 10023,
      "targetPort": 22
    },
    {
      "name": "web",
      "namespace": "web-app",
      "kind": "deployment",
      "workload": "web",
      "selector": "app.kubernetes.io/name=web",
      "listenPort": 10100,
      "targetPort": 8080
    }
  ]
}
```

---

## 5. Multi-Horizon System Roadmap

```
Horizon 1 (Current Core) ──► Horizon 2 (Escapement & Function-Agent)
                                       │
                                       ▼
Horizon 4 (Pluggable Backends) ◄── Horizon 3 (Observability & Probes)
       │
       ▼
Horizon 5 (Multi-Cluster Federation)
```

### Horizon 1: Generic Scale-to-Zero Activator Core (Delivered)
- Dual-workload support: `StatefulSet` and `Deployment` via the `/scale` subresource.
- Independent TCP listeners per configured target.
- Bidirectional zero-copy TCP splicing.
- In-memory idle reaper with annotation opt-in (`unsigned.gg/wake-enabled`).
- OTLP distributed tracing exported to `alloy-gateway.monitoring.svc:4317`.
- Minimal stdlib REST client with zero third-party framework dependencies.

### Horizon 2: Escapement & Function-Agent Integration (EST-34, EST-78, EST-205)
- **Escapement `wake_and_wait` Contract Alignment:** Ensure client dispatchers do not prematurely disconnect while waker holds the TCP accept buffer. Provide deterministic readiness signaling for task dispatch loops.
- **Declarative Function-Agent Registration:** Implement dynamic target registration through Kubernetes annotations (`unsigned.gg/waker-target: "true"`) on Services and Deployments, eliminating the requirement to restart the waker daemon for target updates.
- **Zero-Ingress Originating Agent Support:** Provide scale-up signaling mechanisms for agent workloads that do not listen for ingress traffic but require autonomous wake before outbound task execution.

### Horizon 3: Observability & In-Guest Activity Probing (OPS-672)
- **Prometheus Metrics Exporter:** Dedicated `/metrics` endpoint exporting:
  - `waker_active_connections{target}`: Live gauge of spliced TCP streams.
  - `waker_wake_latency_seconds{target}`: Histogram of cold-boot to TCP-ready timings.
  - `waker_reap_events_total{target}`: Counter of scale-down executions.
  - `waker_saved_core_seconds_total{target}`: Derived capacity conservation metric.
- **In-Guest Activity Probes:** Probe guest agent or apiserver audit logs (`pods/portforward` or active process sessions) before triggering idle reap, preventing connection drops during headless, non-interactive batch jobs.
- **High-Availability Data Path:** Evaluate active-passive leader election and connection preservation protocols to eliminate connection resets during rolling Daemon updates.

### Horizon 4: Pluggable Backends & Transport Adaptation (OPS-673, OPS-1109)
- **`fcsm` Snapshot/Resume Engine:** Abstract the scale controller behind a pluggable `WakeBackend` interface. Support Firecracker microVM snapshot/resume via the `fcsm` session API, cutting wake latencies from ~30s to <1s while preserving memory state.
- **Kata Containers Transport Adapter:** Adapt waker mechanics to support `runtimeClassName: kata-clh` workloads accessed via `kubectl port-forward` directly rather than ClusterIP Services.

### Horizon 5: Multi-Cluster Federation & Predictive Health Gating
- **CrashLoop Health Gate:** Inspect target pod restart counts and termination reasons before attempting scale-up; fast-fail client connections if the backend pod is trapped in CrashLoopBackOff or Unschedulable states.
- **Multi-Cluster Routing:** Coordinate wake activation across geographically distributed clusters (e.g. Cygnus metal and Orion cloud).

---

## 6. Requirements (EARS)

- **[REQ-0001-01]** (Ubiquitous): The waker shall bind an independent TCP listener for each configured target and forward raw bytes without terminating or modifying application encryption envelopes.
- **[REQ-0001-02]** (Event-driven): WHEN an incoming TCP client connection arrives on an inactive target, the waker shall scale the target workload from 0 to 1 via the Kubernetes scale subresource and hold the client connection open in pre-banner state.
- **[REQ-0001-03]** (State-driven): WHILE target pod readiness is pending, the waker shall continuously poll the pod IP and execute non-destructive TCP dials until the backend responds or 120 seconds elapse.
- **[REQ-0001-04]** (Unwanted behavior): IF the backend pod fails to accept a TCP dial within 120 seconds, THEN the waker shall close the client connection and leave target replicas set to 1 for diagnostic custody.
- **[REQ-0001-05]** (State-driven): WHILE a target carries `unsigned.gg/wake-enabled: "true"` and active spliced connections remain at zero for the duration of the configured idle timeout, the idle reaper shall scale target replicas to 0.
- **[REQ-0001-06]** (Event-driven): WHEN the waker daemon restarts, the idle reaper shall reset in-memory activity timestamps for all registered targets, preventing immediate scale-down.
- **[REQ-0001-07]** (Optional): WHERE `FunctionAgent.spec.wake` is declared on a Kubernetes workload, the waker shall dynamically configure proxy listeners from workload annotations without requiring static config updates.
- **[REQ-0001-08]** (Ubiquitous): The waker shall export OpenTelemetry distributed tracing spans for every connection session, scale-up sequence, and idle reaping execution.

---

## 7. Verification & Testing Discipline

1. **Host Throttling (`-j 2`):** Every build and test command on multi-agent workstation `ceres` must enforce bounded execution to prevent CPU starvation.
2. **Race-Free Concurrency:** All Go packages must execute with ThreadSanitizer enabled:
   ```bash
   go test -race -v ./...
   ```
3. **Static Linkage Integrity:** Static binaries must compile without CGO and produce stripped ELF executables:
   ```bash
   CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/waker .
   file /tmp/waker | grep -q 'statically linked'
   ```
4. **Helm Chart Compliance:** Helm templates must validate cleanly against multi-kind fixtures:
   ```bash
   helm lint charts/waker -f charts/waker/ci/test-values.yaml
   helm template waker charts/waker -f charts/waker/ci/test-values.yaml
   ```
5. **Controlled Language (ASD-STE100):** Documentation and architecture specifications must adhere strictly to Sentinel register guidelines with concise, unambiguous phrasing.
