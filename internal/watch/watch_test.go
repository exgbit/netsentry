package watch

import (
	"os"
	"path/filepath"
	"testing"

	"netclient-guard/internal/guardconfig"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name            string
		consistent      bool
		serviceRunning  bool
		backupAvailable bool
		want            Action
	}{
		{"all healthy", true, true, true, ActionNone},
		{"consistent but stopped", true, false, true, ActionStartService},
		{"inconsistent with backup", false, true, true, ActionRestoreAndStart},
		{"inconsistent without backup", false, true, false, ActionAlertNoBackup},
		{"inconsistent stopped with backup", false, false, true, ActionRestoreAndStart},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			load := guardconfig.LoadResult{Consistent: c.consistent}
			if got := Decide(load, c.serviceRunning, c.backupAvailable); got != c.want {
				t.Errorf("Decide(...) = %v, want %v", got, c.want)
			}
		})
	}
}

type fakeService struct {
	running    bool
	startCalls int
	stopCalls  int
}

func (f *fakeService) IsRunning() (bool, error) { return f.running, nil }
func (f *fakeService) Start() error             { f.startCalls++; f.running = true; return nil }
func (f *fakeService) Stop() error              { f.stopCalls++; f.running = false; return nil }

func TestRun_RestoresFromBackupWhenInconsistent(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")
	os.MkdirAll(backupDir, 0o755)

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"broken"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)
	mustWrite(t, filepath.Join(backupDir, "netclient.json.good"), `{"id":"good"}`)
	mustWrite(t, filepath.Join(backupDir, "servers.json.good"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)

	svc := &fakeService{running: true}
	result, err := Run(netclientDir, backupDir, svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != ActionRestoreAndStart {
		t.Fatalf("got action=%v, want ActionRestoreAndStart", result.Action)
	}
	if svc.stopCalls != 1 || svc.startCalls != 1 {
		t.Errorf("expected exactly one stop and one start, got stop=%d start=%d", svc.stopCalls, svc.startCalls)
	}
	restored, _ := os.ReadFile(filepath.Join(netclientDir, "netclient.json"))
	if string(restored) != `{"id":"good"}` {
		t.Errorf("netclient.json was not restored from backup, got %q", restored)
	}
}

func TestRun_AlertsWhenNoBackupAvailable(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"broken"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)

	svc := &fakeService{running: true}
	result, err := Run(netclientDir, backupDir, svc)
	if err == nil {
		t.Fatalf("expected error for no-backup-available alert, got nil (result=%+v)", result)
	}
	if result.Action != ActionAlertNoBackup {
		t.Fatalf("got action=%v, want ActionAlertNoBackup", result.Action)
	}
	if svc.stopCalls != 0 || svc.startCalls != 0 {
		t.Errorf("must not touch service when no backup is available, got stop=%d start=%d", svc.stopCalls, svc.startCalls)
	}
}

func TestRun_StartsStoppedServiceWhenConsistent(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"abc"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	svc := &fakeService{running: false}
	result, err := Run(netclientDir, backupDir, svc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != ActionStartService {
		t.Fatalf("got action=%v, want ActionStartService", result.Action)
	}
	if svc.startCalls != 1 {
		t.Errorf("expected exactly one start call, got %d", svc.startCalls)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
