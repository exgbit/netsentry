package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRun_ConsistentSavesBackup(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"abc"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	outcome, err := Run(netclientDir, backupDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeSaved {
		t.Fatalf("got outcome=%v, want OutcomeSaved", outcome)
	}
	if !fileExists(filepath.Join(backupDir, "netclient.json.good")) {
		t.Errorf("expected netclient.json.good to exist")
	}
	if !fileExists(filepath.Join(backupDir, "servers.json.good")) {
		t.Errorf("expected servers.json.good to exist")
	}
	if !fileExists(filepath.Join(backupDir, "last-good.txt")) {
		t.Errorf("expected last-good.txt to exist")
	}
}

func TestRun_InconsistentSkipsAndDoesNotOverwrite(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")
	os.MkdirAll(backupDir, 0o755)
	mustWrite(t, filepath.Join(backupDir, "netclient.json.good"), "PREVIOUS GOOD")

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"abc"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"different","name":"tomtoc.cn"}}`)

	outcome, err := Run(netclientDir, backupDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeSkippedInconsistent {
		t.Fatalf("got outcome=%v, want OutcomeSkippedInconsistent", outcome)
	}
	data, _ := os.ReadFile(filepath.Join(backupDir, "netclient.json.good"))
	if string(data) != "PREVIOUS GOOD" {
		t.Errorf("existing known-good backup must not be overwritten, got %q", data)
	}
}

func TestRun_MissingFilesSkips(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")

	outcome, err := Run(netclientDir, backupDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeSkippedMissing {
		t.Fatalf("got outcome=%v, want OutcomeSkippedMissing", outcome)
	}
}

func TestRun_MalformedNetclientJSONReturnsZeroOutcome(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{not valid json`)

	outcome, err := Run(netclientDir, backupDir)
	if err == nil {
		t.Fatalf("expected error for malformed netclient.json, got nil")
	}
	if outcome != Outcome(0) {
		t.Fatalf("got outcome=%v, want zero value on error", outcome)
	}
}

// TestRun_SecondFileCopyFailureDoesNotCorruptExistingGoodPair 模拟第二个文件(servers.json)
// 复制失败的场景:通过在 backupDir 下预先放置一个同名目录,让 servers.json.good.tmp 的创建失败。
// 此时第一个文件(netclient.json)已经复制到临时文件,但因为 rename 只在两次复制都成功后才发生,
// 已有的良好备份对必须保持不变,不能出现"一半新一半旧"的情况。
func TestRun_SecondFileCopyFailureDoesNotCorruptExistingGoodPair(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}
	mustWrite(t, filepath.Join(backupDir, "netclient.json.good"), "PREVIOUS GOOD NC")
	mustWrite(t, filepath.Join(backupDir, "servers.json.good"), "PREVIOUS GOOD SRV")

	// 让 servers.json.good.tmp 这个路径已经被一个目录占用,使 copyToTmp 对该路径的 os.Create 失败。
	if err := os.MkdirAll(filepath.Join(backupDir, "servers.json.good.tmp"), 0o755); err != nil {
		t.Fatalf("mkdir conflicting path: %v", err)
	}

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"abc"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	outcome, err := Run(netclientDir, backupDir)
	if err == nil {
		t.Fatalf("expected error when second file copy fails, got nil")
	}
	if outcome != Outcome(0) {
		t.Fatalf("got outcome=%v, want zero value on error", outcome)
	}

	ncData, _ := os.ReadFile(filepath.Join(backupDir, "netclient.json.good"))
	if string(ncData) != "PREVIOUS GOOD NC" {
		t.Errorf("netclient.json.good must not change when servers.json copy fails, got %q", ncData)
	}
	srvData, _ := os.ReadFile(filepath.Join(backupDir, "servers.json.good"))
	if string(srvData) != "PREVIOUS GOOD SRV" {
		t.Errorf("servers.json.good must not change when its own copy fails, got %q", srvData)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
