# CLAUDE.md

Generic scale-to-zero TCP activator (accept-and-hold wake proxy + idle
reaper). See README.md for behavior and config.

## Hard rules

- Stdlib-only Go — no client-go, no external modules. The Kubernetes REST
  client stays minimal and must match the chart's Role verbs exactly
  (`charts/waker/templates/rbac.yaml`); a new API call requires updating
  both in the same commit.
- kebab-case filenames. No `latest` image tags. Images pinned.
- Daemonless builds only: `ko` (see `.ko.yaml`). No Dockerfile, no dockerd.
- Signed commits. PR-required — never push to main.
- Conventional Commits (release-please derives versions; `# x-release-please-version`
  markers in Chart.yaml/values.yaml are load-bearing — do not strip them).
- Chart follows the unsigned-paas chart standard: resources, all three
  probes, restricted securityContext, dedicated SA + minimal RBAC,
  NetworkPolicy, PDB. Keep it compliant even though the paas validators
  don't run here — CI (`helm lint` + `helm template` with
  `charts/waker/ci/test-values.yaml`) is the gate.

## Verify

```bash
go vet ./... && go test ./... && CGO_ENABLED=0 go build -trimpath .
helm lint charts/waker -f charts/waker/ci/test-values.yaml
helm template waker charts/waker -f charts/waker/ci/test-values.yaml
```

## Deploy path

Release here → image `harbor.dev.unsigned.gg/unsigned/waker` + OCI chart
`harbor.dev.unsigned.gg/helm-charts/waker`. unsigned-paas consumes the chart
via `gitops/argocd-apps/waker-app.yaml` (targets + image pin live THERE, in
`valuesObject`). A release is not a deploy; ArgoCD sync is operator-gated.
