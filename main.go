// Command waker is an accept-and-hold TCP wake proxy and idle reaper: a
// scale-to-zero activator for Kubernetes workloads reached over raw TCP
// (SSH being the founding consumer — engineer Firecracker microVMs on the
// unsigned platform, OPS-670).
//
// One TCP listener per configured target. On accept: if the target's pod is
// Ready, dial its backend port and splice bytes; if the workload is scaled
// to zero, scale it to 1 via the scale subresource, wait for the pod IP + a
// successful TCP dial (capped), then splice. A pre-connection client (e.g.
// OpenSSH pre-banner) blocks until the backend answers — no client
// configuration is needed.
//
// An idle reaper scales a target back to zero after `idleTimeout` with zero
// active spliced connections — only when the workload carries the opt-in
// annotation unsigned.gg/wake-enabled: "true". Timers are in-memory: a
// waker restart resets them, which fails toward keeping workloads up, never
// toward severing an active session. On boot-failure the workload is left
// at 1 for operator inspection.
//
// Kubernetes access is a deliberately tiny REST client over the in-cluster
// ServiceAccount (stdlib only, no client-go): GET workload +
// workload/scale, PATCH workload/scale, GET pods (LIST pods for Deployment
// targets) — nothing else, matching the chart's Role exactly.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// version is stamped by the build (-ldflags -X main.version=...).
var version = "dev"

const (
	saDir            = "/var/run/secrets/kubernetes.io/serviceaccount"
	wakeEnabledAnn   = "unsigned.gg/wake-enabled"
	idleTimeoutAnn   = "unsigned.gg/idle-timeout"
	wakeDialCap      = 120 * time.Second
	reapPollInterval = 1 * time.Minute

	kindStatefulSet = "statefulset"
	kindDeployment  = "deployment"
)

// Target is one entry of the JSON config mounted by the chart.
type Target struct {
	Name       string `json:"name"`       // display name, e.g. "aria"
	Namespace  string `json:"namespace"`  // e.g. "aria-vm"
	Kind       string `json:"kind"`       // "statefulset" (default) or "deployment"
	Workload   string `json:"workload"`   // workload object name, e.g. "aria-vm"
	ListenPort int    `json:"listenPort"` // waker-local port
	TargetPort int    `json:"targetPort"` // backend TCP port on the pod
	// Selector is the pod label selector for Deployment targets, whose pod
	// names are not derivable from the workload name (e.g. "app=foo").
	// Ignored for StatefulSets (pod = <workload>-0).
	Selector string `json:"selector,omitempty"`
}

func (t *Target) validate() error {
	if t.Name == "" || t.Namespace == "" || t.Workload == "" {
		return errors.New("name, namespace, and workload are required")
	}
	if t.ListenPort <= 0 || t.TargetPort <= 0 {
		return errors.New("listenPort and targetPort must be positive")
	}
	switch t.Kind {
	case "", kindStatefulSet:
		t.Kind = kindStatefulSet
	case kindDeployment:
		if t.Selector == "" {
			return errors.New("deployment targets require a selector")
		}
	default:
		return fmt.Errorf("unsupported kind %q (statefulset|deployment)", t.Kind)
	}
	return nil
}

// resource returns the apps/v1 resource plural for the target's kind.
func (t *Target) resource() string {
	if t.Kind == kindDeployment {
		return "deployments"
	}
	return "statefulsets"
}

// Config is the mounted waker configuration.
type Config struct {
	Targets            []Target `json:"targets"`
	IdleTimeoutDefault string   `json:"idleTimeoutDefault"` // Go duration, e.g. "45m"
}

// kubeClient is the minimal in-cluster REST client.
type kubeClient struct {
	base  string
	token string
	http  *http.Client
}

func newKubeClient() (*kubeClient, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("not running in-cluster: KUBERNETES_SERVICE_HOST/PORT unset")
	}
	token, err := os.ReadFile(saDir + "/token")
	if err != nil {
		return nil, fmt.Errorf("read serviceaccount token: %w", err)
	}
	caPEM, err := os.ReadFile(saDir + "/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read serviceaccount ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("parse serviceaccount ca.crt")
	}
	return &kubeClient{
		base:  "https://" + net.JoinHostPort(host, port),
		token: strings.TrimSpace(string(token)),
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
	}, nil
}

func (k *kubeClient) do(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, k.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+k.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := k.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (k *kubeClient) getScale(ctx context.Context, t *Target) (int, error) {
	var scale struct {
		Spec struct {
			Replicas int `json:"replicas"`
		} `json:"spec"`
	}
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/%s/%s/scale", t.Namespace, t.resource(), t.Workload)
	if err := k.do(ctx, http.MethodGet, path, "", nil, &scale); err != nil {
		return 0, err
	}
	return scale.Spec.Replicas, nil
}

func (k *kubeClient) setScale(ctx context.Context, t *Target, replicas int) error {
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/%s/%s/scale", t.Namespace, t.resource(), t.Workload)
	body := strings.NewReader(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	return k.do(ctx, http.MethodPatch, path, "application/merge-patch+json", body, nil)
}

func (k *kubeClient) workloadAnnotations(ctx context.Context, t *Target) (map[string]string, error) {
	var obj struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/%s/%s", t.Namespace, t.resource(), t.Workload)
	if err := k.do(ctx, http.MethodGet, path, "", nil, &obj); err != nil {
		return nil, err
	}
	return obj.Metadata.Annotations, nil
}

// podStatus is the subset of pod status the waker reads.
type podStatus struct {
	Status struct {
		PodIP      string `json:"podIP"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}

func (p *podStatus) ipReady() (string, bool) {
	for _, c := range p.Status.Conditions {
		if c.Type == "Ready" && c.Status == "True" {
			return p.Status.PodIP, p.Status.PodIP != ""
		}
	}
	return p.Status.PodIP, false
}

// podEndpoint returns a Ready pod's IP for the target, or ready=false.
// StatefulSets address the ordinal pod by name; Deployments list by the
// configured label selector and take the first Ready pod.
func (k *kubeClient) podEndpoint(ctx context.Context, t *Target) (string, bool, error) {
	if t.Kind == kindStatefulSet {
		var pod podStatus
		path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s-0", t.Namespace, t.Workload)
		if err := k.do(ctx, http.MethodGet, path, "", nil, &pod); err != nil {
			return "", false, err
		}
		ip, ready := pod.ipReady()
		return ip, ready, nil
	}
	var list struct {
		Items []podStatus `json:"items"`
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods?labelSelector=%s", t.Namespace, url.QueryEscape(t.Selector))
	if err := k.do(ctx, http.MethodGet, path, "", nil, &list); err != nil {
		return "", false, err
	}
	for i := range list.Items {
		if ip, ready := list.Items[i].ipReady(); ready {
			return ip, true, nil
		}
	}
	return "", false, nil
}

// targetState tracks one target's connection count and idle clock.
type targetState struct {
	cfg Target

	active       atomic.Int64
	lastActivity atomic.Int64 // unix nano of the last accept or last close
}

func (s *targetState) touch() { s.lastActivity.Store(time.Now().UnixNano()) }

// idleFor reports how long the target has had zero active connections.
func (s *targetState) idleFor() time.Duration {
	if s.active.Load() > 0 {
		return 0
	}
	return time.Since(time.Unix(0, s.lastActivity.Load()))
}

type waker struct {
	kube        *kubeClient
	log         *slog.Logger
	defaultIdle time.Duration
	targets     []*targetState

	ready atomic.Bool
}

// ensureUp scales the target to 1 if needed and waits until its backend
// accepts a TCP dial. Returns the dialable address.
func (w *waker) ensureUp(ctx context.Context, s *targetState) (string, error) {
	replicas, err := w.kube.getScale(ctx, &s.cfg)
	if err != nil {
		return "", fmt.Errorf("get scale: %w", err)
	}
	if replicas < 1 {
		w.log.Info("waking target", "target", s.cfg.Name)
		if err := w.kube.setScale(ctx, &s.cfg, 1); err != nil {
			return "", fmt.Errorf("scale up: %w", err)
		}
	}
	deadline := time.Now().Add(wakeDialCap)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		ip, ready, err := w.kube.podEndpoint(ctx, &s.cfg)
		if err == nil && ready {
			addr := net.JoinHostPort(ip, fmt.Sprint(s.cfg.TargetPort))
			d := net.Dialer{Timeout: 3 * time.Second}
			if c, err := d.DialContext(ctx, "tcp", addr); err == nil {
				c.Close()
				return addr, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	// Deliberately leave the workload scaled up for inspection.
	return "", fmt.Errorf("target %s: not dialable within %s (workload left at 1)", s.cfg.Name, wakeDialCap)
}

func (w *waker) handleConn(ctx context.Context, s *targetState, client net.Conn) {
	defer client.Close()
	s.active.Add(1)
	s.touch()
	defer func() {
		s.active.Add(-1)
		s.touch()
	}()

	addr, err := w.ensureUp(ctx, s)
	if err != nil {
		w.log.Error("wake failed", "target", s.cfg.Name, "err", err)
		return
	}
	backend, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		w.log.Error("backend dial failed", "target", s.cfg.Name, "addr", addr, "err", err)
		return
	}
	defer backend.Close()
	w.log.Info("session open", "target", s.cfg.Name, "client", client.RemoteAddr().String())

	done := make(chan struct{}, 2)
	splice := func(dst, src net.Conn) {
		io.Copy(dst, src) //nolint:errcheck // a torn splice ends the session either way
		if t, ok := dst.(*net.TCPConn); ok {
			t.CloseWrite() //nolint:errcheck
		}
		done <- struct{}{}
	}
	go splice(backend.(*net.TCPConn), client.(*net.TCPConn))
	go splice(client.(*net.TCPConn), backend.(*net.TCPConn))
	<-done
	<-done
	w.log.Info("session closed", "target", s.cfg.Name, "client", client.RemoteAddr().String())
}

func (w *waker) listen(ctx context.Context, s *targetState) error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.ListenPort))
	if err != nil {
		return fmt.Errorf("listen :%d for %s: %w", s.cfg.ListenPort, s.cfg.Name, err)
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	w.log.Info("listening", "target", s.cfg.Name, "port", s.cfg.ListenPort)
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.log.Error("accept failed", "target", s.cfg.Name, "err", err)
			continue
		}
		go w.handleConn(ctx, s, conn)
	}
}

// reapOnce scales down every opted-in target whose idle clock has expired.
func (w *waker) reapOnce(ctx context.Context) {
	for _, s := range w.targets {
		ann, err := w.kube.workloadAnnotations(ctx, &s.cfg)
		if err != nil {
			w.log.Error("annotation read failed", "target", s.cfg.Name, "err", err)
			continue
		}
		if ann[wakeEnabledAnn] != "true" {
			continue // opted out — never reap
		}
		idle := w.defaultIdle
		if v := ann[idleTimeoutAnn]; v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				idle = d
			} else {
				w.log.Warn("bad idle-timeout annotation, using default", "target", s.cfg.Name, "value", v)
			}
		}
		if s.idleFor() < idle {
			continue
		}
		replicas, err := w.kube.getScale(ctx, &s.cfg)
		if err != nil || replicas == 0 {
			continue
		}
		w.log.Info("reaping idle target", "target", s.cfg.Name, "idle", s.idleFor().Round(time.Second).String())
		if err := w.kube.setScale(ctx, &s.cfg, 0); err != nil {
			w.log.Error("scale down failed", "target", s.cfg.Name, "err", err)
		}
	}
}

func (w *waker) health(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(rw http.ResponseWriter, _ *http.Request) {
		if w.ready.Load() {
			rw.WriteHeader(http.StatusOK)
			return
		}
		rw.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.ListenAndServe() //nolint:errcheck // fatal-path surfaces via readiness
	return srv
}

// loadConfig reads, parses, and validates the mounted configuration.
func loadConfig(path string) (*Config, time.Duration, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, 0, fmt.Errorf("parse config: %w", err)
	}
	// Empty targets is allowed: an orchestrator must be deployable before
	// any consumer exists. main() logs a warning; the reaper + health
	// server run idle until targets are added (via a redeploy with config).
	seen := map[int]string{}
	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		if err := t.validate(); err != nil {
			return nil, 0, fmt.Errorf("target %q: %w", t.Name, err)
		}
		if prev, dup := seen[t.ListenPort]; dup {
			return nil, 0, fmt.Errorf("target %q: listenPort %d already used by %q", t.Name, t.ListenPort, prev)
		}
		seen[t.ListenPort] = t.Name
	}
	defaultIdle := 45 * time.Minute
	if cfg.IdleTimeoutDefault != "" {
		d, err := time.ParseDuration(cfg.IdleTimeoutDefault)
		if err != nil || d <= 0 {
			return nil, 0, fmt.Errorf("bad idleTimeoutDefault %q", cfg.IdleTimeoutDefault)
		}
		defaultIdle = d
	}
	return &cfg, defaultIdle, nil
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfgPath := os.Getenv("WAKER_CONFIG")
	if cfgPath == "" {
		cfgPath = "/etc/waker/config.json"
	}
	cfg, defaultIdle, err := loadConfig(cfgPath)
	if err != nil {
		log.Error("config", "path", cfgPath, "err", err)
		os.Exit(1)
	}
	kube, err := newKubeClient()
	if err != nil {
		log.Error("kube client", "err", err)
		os.Exit(1)
	}

	w := &waker{kube: kube, log: log, defaultIdle: defaultIdle}
	for _, tc := range cfg.Targets {
		s := &targetState{cfg: tc}
		s.touch() // restart grace: idle clock starts NOW, never in the past
		w.targets = append(w.targets, s)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := w.health(":8080")
	var wg sync.WaitGroup
	for _, s := range w.targets {
		wg.Add(1)
		go func(s *targetState) {
			defer wg.Done()
			if err := w.listen(ctx, s); err != nil {
				log.Error("listener failed", "target", s.cfg.Name, "err", err)
				stop() // a dead listener means partial service — restart the pod
			}
		}(s)
	}
	go func() {
		t := time.NewTicker(reapPollInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.reapOnce(ctx)
			}
		}
	}()
	w.ready.Store(true)
	if len(w.targets) == 0 {
		log.Warn("no targets configured — running idle; reaper + health server active until targets are added")
	}

	<-ctx.Done()
	w.ready.Store(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx) //nolint:errcheck
	wg.Wait()
}
