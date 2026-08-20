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

// TestCollect_ConfigReadError_StaysConfiguredNotHealthy 是真机踩过的坑:
// netclient.json 存在但读取/解析这一下失败(比如被别的进程短暂占用——面板每 3
// 秒轮询一次,撞上这种瞬时失败不是小概率事件),guardconfig.Load 会返回一个
// 非 nil error;此时 Collect 不能把 Configured 报成 false,不然前端会把已经
// 配置好的机器错误地打回"输入 token 安装"的表单,像是把配置清空了一样。
func TestCollect_ConfigReadError_StaysConfiguredNotHealthy(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := t.TempDir()

	// 文件存在,但内容不是合法 JSON——触发 guardconfig.Load 的解析错误分支
	// (不是"文件不存在"那种被 guardconfig.Load 自己吞掉、不算错误的分支)。
	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{not valid json`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)

	got, err := Collect(netclientDir, backupDir, fakeService{running: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Configured {
		t.Errorf("Configured = false, want true (file exists, only the read/parse failed)")
	}
	if got.Healthy {
		t.Errorf("Healthy = true, want false (couldn't confirm consistency this round)")
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
