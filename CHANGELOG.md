# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
