package trayui

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeService 是 Collect 所需窄接口的假实现,供测试使用。
type fakeService struct {
	running bool
	err     error
}

func (f fakeService) IsRunning() (bool, error) { return f.running, f.err }

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollect_NotConfigured(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := t.TempDir()

	got, err := Collect(netclientDir, backupDir, fakeService{running: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Configured {
		t.Errorf("Configured = true, want false (no netclient.json)")
	}
	if got.Healthy {
		t.Errorf("Healthy = true, want false")
	}
}

func TestCollect_ConsistentAndRunning_Healthy(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := t.TempDir()

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"good"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)

	got, err := Collect(netclientDir, backupDir, fakeService{running: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Configured {
		t.Errorf("Configured = false, want true")
	}
	if !got.Healthy {
		t.Errorf("Healthy = false, want true")
	}
	if got.ServerName != "tomtoc.cn" {
		t.Errorf("ServerName = %q, want tomtoc.cn", got.ServerName)
	}
	if got.ServiceStatus != "Running" {
		t.Errorf("ServiceStatus = %q, want Running", got.ServiceStatus)
	}
}

func TestCollect_InconsistentConfig_Unhealthy(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := t.TempDir()

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"broken"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)

	got, err := Collect(netclientDir, backupDir, fakeService{running: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Healthy {
		t.Errorf("Healthy = true, want false (inconsistent config)")
	}
}

func TestCollect_ServiceNotRunning_Unhealthy(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := t.TempDir()

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"good"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)

	got, err := Collect(netclientDir, backupDir, fakeService{running: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Healthy {
		t.Errorf("Healthy = true, want false (service stopped)")
	}
	if got.ServiceStatus != "Stopped" {
		t.Errorf("ServiceStatus = %q, want Stopped", got.ServiceStatus)
	}
}

func TestCollect_ServiceStatusError_UnknownAndUnhealthy(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := t.TempDir()

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"good"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)

	got, err := Collect(netclientDir, backupDir, fakeService{err: os.ErrPermission})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ServiceStatus != "Unknown" {
		t.Errorf("ServiceStatus = %q, want Unknown", got.ServiceStatus)
	}
	if got.Healthy {
		t.Errorf("Healthy = true, want false (service status unknown)")
	}
}

func TestCollect_LastBackup_FromFile(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := t.TempDir()

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"good"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)
	mustWrite(t, filepath.Join(backupDir, "last-good.txt"), "2026-08-19T10:00:00Z")

	got, err := Collect(netclientDir, backupDir, fakeService{running: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastBackup != "2026-08-19T10:00:00Z" {
		t.Errorf("LastBackup = %q, want 2026-08-19T10:00:00Z", got.LastBackup)
	}
}

func TestCollect_LastBackup_MissingIsEmpty(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := t.TempDir()

	got, err := Collect(netclientDir, backupDir, fakeService{running: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastBackup != "" {
		t.Errorf("LastBackup = %q, want empty", got.LastBackup)
	}
}
