# waker — Roadmap

> Accept-and-hold TCP wake proxy + idle reaper — scale-to-zero activator.
> Repo: `unsigned-gg/waker` · Codename: portolan (cartography — the routing chart)

## Current state (2026-07-24)

- **v1.0.0** — deployed, functional
- One TCP listener per target, splice-on-ready, idle reaper with annotation opt-in
- ko-built + cosign-signed image, Helm chart published to Harbor
- ArgoCD deployment via unsigned-paas

## Integration with soma

- **escapement → waker**: dispatch to scale-to-zero microVM targets
  - Escapement triggers waker's TCP wake proxy before dispatching
  - Job waits for pod Ready (capped at 120s), then dispatches
  - Waker's idle reaper eventually scales back to zero

- **runtime → waker**: function agents on Firecracker microVMs
  - Engineer VMs (OPS-670) scale to zero via waker
  - Escapement dispatches to these targets through the wake-and-wait flow

## Roadmap
- **Escapement wake integration** (EST-34) — `wake_and_wait(target) -> Ready`
- **Multi-cluster wake** — wake targets across Cygnus/Orion
- **Health-gated wake** — refuse to wake if workload is in a CrashLoopBackOff
