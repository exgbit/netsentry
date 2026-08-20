package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"netsentry/internal/guardconfig"
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

// fakeService 是 ServiceController 的假实现,供测试使用。
//
// logSequences[i] 描述第 i+1 次 Start() 调用之后,ReadLogFrom 依次返回的日志内容:每调用一次
// ReadLogFrom 就往后取序列里的下一项,取到末尾后重复最后一项。某次 Start 对应的序列缺失
// (nil,即默认零值)时,ReadLogFrom 立刻返回包含 brokerConnectedSignal 的内容,模拟"启动后
// 立刻健康"——这样不关心 broker 健康检查的既有测试无需改动就能继续通过。
type fakeService struct {
	running    bool
	startCalls int
	stopCalls  int

	logSequences [][][]byte
	pollCounts   []int
}

func (f *fakeService) IsRunning() (bool, error) { return f.running, nil }

func (f *fakeService) Start() error {
	f.startCalls++
	f.running = true
	f.pollCounts = append(f.pollCounts, 0)
	return nil
}

func (f *fakeService) Stop() error { f.stopCalls++; f.running = false; return nil }

func (f *fakeService) LogSize() (int64, error) { return 0, nil }

func (f *fakeService) ReadLogFrom(offset int64) ([]byte, error) {
	attempt := f.startCalls
	if attempt == 0 {
		return nil, nil
	}
	if len(f.logSequences) < attempt || f.logSequences[attempt-1] == nil {
		return []byte(brokerConnectedSignal), nil
	}
	seq := f.logSequences[attempt-1]
	idx := f.pollCounts[attempt-1]
	if idx >= len(seq) {
		idx = len(seq) - 1
	}
	f.pollCounts[attempt-1]++
	return seq[idx], nil
}

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

func TestRun_LeavesServiceStoppedOnPartialRestoreFailure(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")
	os.MkdirAll(backupDir, 0o755)

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"broken"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)
	mustWrite(t, filepath.Join(backupDir, "netclient.json.good"), `{"id":"good"}`)
	// servers.json.good 是一个目录而不是文件,让 copyOverwrite 里的 os.ReadFile 在复制第二个
	// 文件时报错,模拟 netclient.json 已恢复、servers.json 恢复失败的部分恢复场景。
	if err := os.MkdirAll(filepath.Join(backupDir, "servers.json.good"), 0o755); err != nil {
		t.Fatalf("mkdir servers.json.good: %v", err)
	}

	svc := &fakeService{running: true}
	result, err := Run(netclientDir, backupDir, svc)
	if err == nil {
		t.Fatalf("expected error from failed servers.json restore, got nil (result=%+v)", result)
	}
	if svc.startCalls != 0 {
		t.Errorf("service must not be started against a half-restored config, got startCalls=%d", svc.startCalls)
	}
	if svc.stopCalls != 1 {
		t.Errorf("expected exactly one stop call, got %d", svc.stopCalls)
	}
	restored, readErr := os.ReadFile(filepath.Join(netclientDir, "netclient.json"))
	if readErr != nil {
		t.Fatalf("read netclient.json: %v", readErr)
	}
	if string(restored) != `{"id":"good"}` {
		t.Errorf("netclient.json should have been overwritten with the backup's content before the servers.json copy failed, got %q", restored)
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

func TestIsBrokerConnected(t *testing.T) {
	cases := []struct {
		name string
		tail []byte
		want bool
	}{
		{"contains signal", []byte(`{"level":"info","msg":"mqtt connect handler"}`), true},
		{"unrelated content", []byte(`{"level":"warn","msg":"unable to connect to broker"}`), false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsBrokerConnected(c.tail); got != c.want {
				t.Errorf("IsBrokerConnected(%q) = %v, want %v", c.tail, got, c.want)
			}
		})
	}
}

func TestStartAndVerifyHealthy_SucceedsOnFirstAttempt(t *testing.T) {
	svc := &fakeService{running: false}
	err := startAndVerifyHealthy(svc, 2, 50*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.startCalls != 1 {
		t.Errorf("expected exactly one start call, got %d", svc.startCalls)
	}
	if svc.stopCalls != 0 {
		t.Errorf("expected no stop calls, got %d", svc.stopCalls)
	}
}

func TestStartAndVerifyHealthy_RetriesAndSucceedsOnSecondAttempt(t *testing.T) {
	svc := &fakeService{
		running: false,
		// 第一次 Start 之后的每次 ReadLogFrom 都没有信号,直到超时;第二次 Start 之后立刻有信号。
		logSequences: [][][]byte{
			{[]byte("unable to connect to broker")},
			{[]byte("mqtt connect handler")},
		},
	}
	err := startAndVerifyHealthy(svc, 3, 30*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.startCalls != 2 {
		t.Errorf("expected exactly two start calls, got %d", svc.startCalls)
	}
	if svc.stopCalls != 1 {
		t.Errorf("expected exactly one stop call before the retry, got %d", svc.stopCalls)
	}
}

func TestStartAndVerifyHealthy_FailsAfterMaxAttempts(t *testing.T) {
	svc := &fakeService{
		running: false,
		logSequences: [][][]byte{
			{[]byte("unable to connect to broker")},
			{[]byte("unable to connect to broker")},
		},
	}
	err := startAndVerifyHealthy(svc, 2, 20*time.Millisecond, 5*time.Millisecond)
	if err == nil {
		t.Fatalf("expected error after exhausting all attempts, got nil")
	}
	if svc.startCalls != 2 {
		t.Errorf("expected exactly two start calls, got %d", svc.startCalls)
	}
	if !strings.Contains(err.Error(), "broker") {
		t.Errorf("expected error to mention the broker connectivity issue, got %q", err)
	}

	// Run 用 fmt.Errorf("start service: %w", err) / "start service after restore: %w" 包装这个
	// 错误(watch.go 里可见),%w 保留原始信息,所以包装后的错误文本里 broker 相关的细节依然完整,
	// 足够在 guard.log 里定位问题。这里直接复现同样的包装方式来确认这一点。
	wrapped := fmt.Errorf("start service: %w", err)
	if !strings.Contains(wrapped.Error(), "broker") {
		t.Errorf("expected Run's wrapped error to still mention broker, got %q", wrapped)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
