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
