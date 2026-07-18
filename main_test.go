package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdleForZeroWhileActive(t *testing.T) {
	s := &targetState{}
	s.touch()
	s.active.Add(1)
	if got := s.idleFor(); got != 0 {
		t.Fatalf("idleFor with active connection = %v, want 0", got)
	}
}

func TestIdleForCountsFromLastActivity(t *testing.T) {
	s := &targetState{}
	s.lastActivity.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	if got := s.idleFor(); got < 9*time.Minute {
		t.Fatalf("idleFor = %v, want >= 9m", got)
	}
}

func TestTouchResetsIdleClock(t *testing.T) {
	s := &targetState{}
	s.lastActivity.Store(time.Now().Add(-time.Hour).UnixNano())
	s.touch()
	if got := s.idleFor(); got > time.Second {
		t.Fatalf("idleFor after touch = %v, want ~0", got)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigDefaultsKindToStatefulSet(t *testing.T) {
	cfg, idle, err := loadConfig(writeConfig(t, `{
		"targets": [{"name":"a","namespace":"a-vm","workload":"a-vm","listenPort":10022,"targetPort":22}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Targets[0].Kind != kindStatefulSet {
		t.Fatalf("kind = %q, want %q", cfg.Targets[0].Kind, kindStatefulSet)
	}
	if idle != 45*time.Minute {
		t.Fatalf("default idle = %v, want 45m", idle)
	}
}

func TestLoadConfigDeploymentRequiresSelector(t *testing.T) {
	_, _, err := loadConfig(writeConfig(t, `{
		"targets": [{"name":"d","namespace":"d","kind":"deployment","workload":"d","listenPort":10022,"targetPort":22}]
	}`))
	if err == nil {
		t.Fatal("deployment target without selector accepted, want error")
	}
}

func TestLoadConfigRejectsUnknownKind(t *testing.T) {
	_, _, err := loadConfig(writeConfig(t, `{
		"targets": [{"name":"x","namespace":"x","kind":"daemonset","workload":"x","listenPort":10022,"targetPort":22}]
	}`))
	if err == nil {
		t.Fatal("daemonset kind accepted, want error")
	}
}

func TestLoadConfigRejectsDuplicateListenPorts(t *testing.T) {
	_, _, err := loadConfig(writeConfig(t, `{
		"targets": [
			{"name":"a","namespace":"a","workload":"a","listenPort":10022,"targetPort":22},
			{"name":"b","namespace":"b","workload":"b","listenPort":10022,"targetPort":22}
		]
	}`))
	if err == nil {
		t.Fatal("duplicate listenPort accepted, want error")
	}
}

func TestLoadConfigRejectsEmptyTargets(t *testing.T) {
	_, _, err := loadConfig(writeConfig(t, `{"targets": []}`))
	if err == nil {
		t.Fatal("empty targets accepted, want error")
	}
}

func TestTargetResourcePlural(t *testing.T) {
	sts := &Target{Kind: kindStatefulSet}
	if got := sts.resource(); got != "statefulsets" {
		t.Fatalf("statefulset resource = %q", got)
	}
	dep := &Target{Kind: kindDeployment}
	if got := dep.resource(); got != "deployments" {
		t.Fatalf("deployment resource = %q", got)
	}
}
