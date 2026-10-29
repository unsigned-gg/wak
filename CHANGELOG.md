# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.2.0](https://github.com/unsigned-gg/wak/compare/v1.1.0...v1.2.0) (2026-10-29)


### Features

* **deploy:** add ArgoCD Application manifest (git-path chart source) ([#4](https://github.com/unsigned-gg/wak/issues/4)) ([e59ea34](https://github.com/unsigned-gg/wak/commit/e59ea34f41aaf99847a3e36acf85c0a74fcd4d7f))
* generic scale-to-zero TCP activator — extracted from unsigned-paas engineer-vm-waker (OPS-670) ([a28b289](https://github.com/unsigned-gg/wak/commit/a28b289226ea050cf251fe6f3d6b78fb8be4a966))
* OTLP distributed tracing (v1.1.0) ([#10](https://github.com/unsigned-gg/wak/issues/10)) ([bd62d2f](https://github.com/unsigned-gg/wak/commit/bd62d2fd22a68baba35a9d4bf9d4a32568a457da))
* **wak:** land waker accept-and-hold TCP proxy under 3-letter namespace and CC0 license ([12cc229](https://github.com/unsigned-gg/wak/commit/12cc2294c19e7cb42f651e961efeaf8bebda7ab2))


### Bug Fixes

* **chart:** allow soma namespace health probes on :8080 ([#8](https://github.com/unsigned-gg/wak/issues/8)) ([b3f1555](https://github.com/unsigned-gg/wak/commit/b3f155520f35fe999e8aadc1d4cd1ae52c60ef77))
* **chart:** guard service target ports on non-empty targets ([#11](https://github.com/unsigned-gg/wak/issues/11)) ([4a6ffc5](https://github.com/unsigned-gg/wak/commit/4a6ffc51dc107f751fcf7655322cb41d03f96a6a))
* **chart:** guard wake ingress on non-empty targets, fix health rule ([#9](https://github.com/unsigned-gg/wak/issues/9)) ([67923a9](https://github.com/unsigned-gg/wak/commit/67923a932413580405e5d3e08c5fb369534c2952))
* **deploy:** image tag v1.0.0 (match built image ref) ([#6](https://github.com/unsigned-gg/wak/issues/6)) ([860a876](https://github.com/unsigned-gg/wak/commit/860a8761b3e94c59ffb0eb0e9b129b177bf997bd))
* stay up on empty targets (deployable before consumers arrive) ([#7](https://github.com/unsigned-gg/wak/issues/7)) ([4140567](https://github.com/unsigned-gg/wak/commit/4140567435f0950ee57656dfa50fdc38db12d90a))

## [1.1.0] - 2026-07-24

### Added

- OTLP distributed tracing via `go.opentelemetry.io/otel` — exports spans to
  `alloy-gateway.monitoring.svc:4317` (OTLP gRPC). Service name `waker`,
  resource attributes include service version. Tracing can be disabled via
  `OTEL_TRACES_ENABLED=false`.
- Spans on the critical path: `waker.session` (root, per TCP connection),
  `waker.ensure_up` (scale-to-ready cycle), `waker.reap` (per-target idle
  reaping). Each carries target name, kind, replica count, readiness, idle
  duration, and error attributes.
- Traceparent propagation on health endpoints: `/healthz` and `/readyz`
  extract inbound `traceparent` headers so probes from the soma-operator or
  monitoring continue a trace rather than creating orphan spans.
- Chart `tracing` values block: `tracing.enabled` (default true),
  `tracing.endpoint` (default `alloy-gateway.monitoring.svc:4317`). Renders
  `OTEL_TRACES_ENABLED` + `OTEL_EXPORTER_OTLP_ENDPOINT` env vars.

### Changed

- CLAUDE.md: stdlib-only rule relaxed — `go.opentelemetry.io/*` is now the
  sole permitted external dependency (for OTLP tracing). K8s REST client
  remains stdlib-only.

## [1.0.1] - 2026-07-24

### Changed

- Empty targets config no longer exits the process. An orchestrator must be
  deployable before any consumer exists: with 0 targets waker logs a warning
  and stays up — the idle reaper loop and health server remain active. Targets
  are added via a redeploy with config (listeners open per target). Missing
  config file is still a hard error (exits 1).

## [1.0.0] - 2026-07-24

First stable release of waker — the accept-and-hold TCP wake proxy + idle
reaper, a scale-to-zero activator for Kubernetes StatefulSets and Deployments
reached over raw TCP. Founding consumer: engineer Firecracker microVMs on the
unsigned platform (OPS-670).

### Added

- Accept-and-hold TCP wake proxy: one listener per configured target. On accept,
  if the target pod is Ready, dial its backend port and splice bytes; if scaled
  to zero, scale to 1 via the scale subresource, wait for pod IP + successful
  TCP dial (capped at 120 s), then splice. Pre-connection clients (e.g. OpenSSH
  pre-banner) block until the backend answers — no client configuration needed.
- Idle reaper: scales a target to zero after `idleTimeout` (default 45 m) with
  zero active spliced connections — only when the workload carries the opt-in
  annotation `unsigned.gg/wake-enabled: "true"`. Per-workload override via
  `unsigned.gg/idle-timeout`. Timers are in-memory (restart resets them, failing
  toward keeping workloads up). Boot failures leave the workload at 1 for
  inspection.
- Minimal stdlib-only Kubernetes REST client (no client-go): GET workload,
  workload/scale, PATCH workload/scale, GET pods — matching the chart's Role
  verbs exactly.
- Helm chart (`charts/waker`): Deployment, Service, config ConfigMap,
  ServiceAccount + minimal RBAC (per-target-namespace Roles, no wildcards),
  NetworkPolicy, PDB, optional ExternalSecret for the Harbor pull robot.
- ko-built + (cosign-signed) image, Helm chart published to Harbor.
- ArgoCD Application manifest (`deploy/gitops/waker-app.yaml`): standalone app,
  manual sync, git-path chart source, CreateNamespace + ServerSideApply.
- Roadmap with escapement/soma integration direction (EST-34).
- CI: Go vet + test + build, Helm lint + template (dual-kind fixture).

### Changed

- Extracted from unsigned-paas engineer-vm-waker as a standalone repo
  (unsigned-gg/waker), generalized from StatefulSet-only to Deployment+StatefulSet.
- Image/chart references updated to prod Harbor paths
  (`harbor.unsigned.gg/platform/waker`).
- `.blackwall/` added to `.gitignore` (estate-wide sweep; waker was missed).
