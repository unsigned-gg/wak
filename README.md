# wak (waker)

[![License: CC0-1.0](https://img.shields.io/badge/license-CC0--1.0-blue.svg)](LICENSE)

> Accept-and-hold TCP wake proxy + idle reaper: a scale-to-zero activator for
> Kubernetes StatefulSets and Deployments reached over raw TCP. Part of the `unsigned` 3-letter systems matrix.
> Founding consumer: engineer Firecracker microVMs on the unsigned platform (`unsigned-paas`).


## How it works

One TCP listener per configured target. On accept:

- pod Ready → dial its backend port, splice bytes (SSH/TLS end-to-end crypto
  is untouched — the waker never terminates anything).
- workload scaled to zero → `PATCH scale` to 1, poll for a Ready pod and a
  successful TCP dial (capped at 120 s), then splice. Pre-connection clients
  (e.g. OpenSSH pre-banner) block until the backend answers — no client
  configuration needed.

An idle reaper scales a target to zero after `idleTimeout` with zero active
spliced connections — only when the workload carries the opt-in annotation
`unsigned.gg/wake-enabled: "true"` (per-workload override:
`unsigned.gg/idle-timeout: "45m"`). Idle timers are in-memory; a waker
restart resets them (fails toward keeping workloads up). Boot failures leave
the workload at 1 for inspection.

## Configuration

JSON mounted by the chart (`WAKER_CONFIG`, default `/etc/waker/config.json`):

```json
{
  "idleTimeoutDefault": "45m",
  "targets": [
    {"name": "aria", "namespace": "aria-vm", "workload": "aria-vm",
     "listenPort": 10023, "targetPort": 22},
    {"name": "web", "namespace": "web-app", "kind": "deployment",
     "workload": "web", "selector": "app.kubernetes.io/name=web",
     "listenPort": 10100, "targetPort": 8080}
  ]
}
```

`kind` defaults to `statefulset` (pod = `<workload>-0`). Deployment targets
require `selector` for pod discovery. Listing a target only opens a
listener — nothing changes for a workload until its Service is repointed at
the waker and it opts into reaping via the annotation.

## Chart

`charts/waker` — published as OCI to `harbor.unsigned.gg/helm-charts` by
the release workflow, alongside the ko-built, cosign-signed image
`harbor.unsigned.gg/platform/waker`. Deployments set `targets` (and RBAC
follows: per-target-namespace Roles, no wildcards, nothing cluster-scoped).

## Development

```bash
go vet ./... && go test ./...
helm lint charts/waker -f charts/waker/ci/test-values.yaml
```

Releases: Conventional Commits → release-please → tag → publish workflow.
A release is not a deploy — cluster rollout is the consuming platform
repo's ArgoCD pin, operator-gated.

## License

Dedicated to the public domain under [CC0 1.0 Universal](LICENSE).

This repository follows the unsigned constitution: [Privacy, Freedom, Dignity](https://github.com/unsigned-gg/.github/blob/main/PRIVACY_FREEDOM_DIGNITY.md) · [Guiding Principles](https://github.com/unsigned-gg/.github/blob/main/GUIDING_PRINCIPLES.md) · [Community Standards](https://github.com/unsigned-gg/.github/blob/main/COMMUNITY_STANDARDS.md) · [Brand Voice](https://github.com/unsigned-gg/.github/blob/main/BRAND_VOICE.md).
