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
		brokerStuck     bool
		want            Action
	}{
		{"all healthy", true, true, true, false, ActionNone},
		{"consistent but stopped", true, false, true, false, ActionStartService},
		{"inconsistent with backup", false, true, true, false, ActionRestoreAndStart},
		{"inconsistent without backup", false, true, false, false, ActionAlertNoBackup},
		{"inconsistent stopped with backup", false, false, true, false, ActionRestoreAndStart},
		{"running but stuck reconnecting to broker", true, true, true, true, ActionRestartStuckService},
		{"stopped takes priority over stuck flag", true, false, true, true, ActionStartService},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			load := guardconfig.LoadResult{Consistent: c.consistent}
			if got := Decide(load, c.serviceRunning, c.backupAvailable, c.brokerStuck); got != c.want {
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

	// steadyStateLog 是 Run() 在决定要不要 Start 之前、检查"服务是不是卡在跟
	// broker 断线"时读到的日志内容——这个检查发生在 startCalls 还是 0 的时候
	// (还没决定要不要 Start,不涉及 Start 之后的健康检查轮询)。默认零值 nil,
	// 配合 IsBrokerStuck(nil)==false(没有卡死信号),不影响原本不关心这个检查
	// 的既有测试。
	steadyStateLog []byte
}

func (f *fakeService) IsRunning() (bool, error) { return f.running, nil }

func (f *fakeService) Start() error {
	f.startCalls++
	f.running = true
	f.pollCounts = append(f.pollCounts, 0)
	return nil
}

func (f *fakeService) Stop() error { f.stopCalls++; f.running = false; return nil }

func (f *fakeService) LogSize() (int64, error) { return int64(len(f.steadyStateLog)), nil }

func (f *fakeService) ReadLogFrom(offset int64) ([]byte, error) {
	attempt := f.startCalls
	if attempt == 0 {
		return f.steadyStateLog, nil
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

// fakeTunnel 是 TunnelChecker 的假实现,供测试使用。零值(reachable: false)对应"隧道也确认
// 不通",配合 IsBrokerStuck 触发重启;reachable: true 对应"隧道其实是通的"这个真机事故复盘出
// 来的场景,用来验证这种情况下不会去碰服务。
type fakeTunnel struct{ reachable bool }

func (f fakeTunnel) TunnelReachable() bool { return f.reachable }

func TestRun_RestoresFromBackupWhenInconsistent(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")
	os.MkdirAll(backupDir, 0o755)

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"broken"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)
	mustWrite(t, filepath.Join(backupDir, "netclient.json.good"), `{"id":"good"}`)
	mustWrite(t, filepath.Join(backupDir, "servers.json.good"), `{"tomtoc.cn":{"mqid":"good","name":"tomtoc.cn"}}`)

	svc := &fakeService{running: true}
	result, err := Run(netclientDir, backupDir, svc, fakeTunnel{})
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
	result, err := Run(netclientDir, backupDir, svc, fakeTunnel{})
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
	result, err := Run(netclientDir, backupDir, svc, fakeTunnel{})
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
	result, err := Run(netclientDir, backupDir, svc, fakeTunnel{})
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

func TestRun_NoActionWhenRunningAndBrokerHealthy(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"abc"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	svc := &fakeService{running: true, steadyStateLog: []byte("...mqtt connect handler...")}
	result, err := Run(netclientDir, backupDir, svc, fakeTunnel{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != ActionNone {
		t.Fatalf("got action=%v, want ActionNone", result.Action)
	}
	if svc.startCalls != 0 || svc.stopCalls != 0 {
		t.Errorf("must not touch a healthy running service, got stop=%d start=%d", svc.stopCalls, svc.startCalls)
	}
}

// TestRun_RestartsServiceStuckReconnectingToBroker 复现真机诊断包发现的场景:sc query
// 显示 Running、配置一致,winsw.out.log 里最后一次跟 broker 相关的信号是失败,而且 ping
// 内网目标也确认不通(fakeTunnel{reachable: false})——这时候才真正值得重启修复。
func TestRun_RestartsServiceStuckReconnectingToBroker(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"abc"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	svc := &fakeService{
		running:        true,
		steadyStateLog: []byte("unable to connect to broker, retrying ..."),
	}
	result, err := Run(netclientDir, backupDir, svc, fakeTunnel{reachable: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != ActionRestartStuckService {
		t.Fatalf("got action=%v, want ActionRestartStuckService", result.Action)
	}
	if svc.stopCalls != 1 || svc.startCalls != 1 {
		t.Errorf("expected exactly one stop and one start, got stop=%d start=%d", svc.stopCalls, svc.startCalls)
	}
}

// TestRun_DoesNotRestartWhenBrokerStuckButTunnelReachable 是真机事故复盘出来的回归测试:
// 2026-08-21 部署过一版没有这个门槛检查的代码,真机上日志判定"卡死"、直接重启了服务,
// 结果连着 3 分钟反复 Stop+Start 却始终没能让 broker 重新连上(说明这次根本不是"重启能
// 解开的卡死",而是 broker 那边本身就连不上),期间还让原本工作正常的隧道跟着抖动、
// SSH 断续了好几次。回退后确认:ping 内网目标全程都是通的——broker 卡死不等于隧道跟着
// 坏。这个测试锁定这个行为:隧道确认可达时,哪怕日志显示卡死,也不能去碰服务。
func TestRun_DoesNotRestartWhenBrokerStuckButTunnelReachable(t *testing.T) {
	netclientDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backup")

	mustWrite(t, filepath.Join(netclientDir, "netclient.json"), `{"id":"abc"}`)
	mustWrite(t, filepath.Join(netclientDir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	svc := &fakeService{
		running:        true,
		steadyStateLog: []byte("unable to connect to broker, retrying ..."),
	}
	result, err := Run(netclientDir, backupDir, svc, fakeTunnel{reachable: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != ActionNone {
		t.Fatalf("got action=%v, want ActionNone (tunnel is reachable, must not restart)", result.Action)
	}
	if svc.stopCalls != 0 || svc.startCalls != 0 {
		t.Errorf("must not touch the service when the tunnel is reachable, got stop=%d start=%d", svc.stopCalls, svc.startCalls)
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

func TestIsBrokerStuck(t *testing.T) {
	cases := []struct {
		name string
		tail []byte
		want bool
	}{
		{"only failure signal", []byte("unable to connect to broker, retrying ..."), true},
		{"only success signal", []byte(`{"msg":"mqtt connect handler"}`), false},
		{"failure then later success", []byte("unable to connect to broker\n...\nmqtt connect handler"), false},
		{"success then later failure", []byte("mqtt connect handler\n...\nunable to connect to broker, retrying"), true},
		{"neither signal present", []byte("completed pull for server tomtoc.cn"), false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsBrokerStuck(c.tail); got != c.want {
				t.Errorf("IsBrokerStuck(%q) = %v, want %v", c.tail, got, c.want)
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

// TestStartAndVerifyHealthy_PassesWhenNoSignalAtAll 是真机踩坑后的回归用例:
// netclient v1.6.0 join 出来的主机默认 verbosity 0,成功信号(slog.Info 级)整个
// 被过滤,broker 实际连上了日志里也永远看不到那一行——这种情况下超时不能判失败,
// 只要没有失败证据(ERROR 级、任何 verbosity 都会打)就该放行,更不能去反复重启
// 一个其实健康的服务。
func TestStartAndVerifyHealthy_PassesWhenNoSignalAtAll(t *testing.T) {
	svc := &fakeService{
		running: false,
		logSequences: [][][]byte{
			{[]byte("completed pull for server tomtoc.cn")},
		},
	}
	err := startAndVerifyHealthy(svc, 3, 30*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.startCalls != 1 {
		t.Errorf("expected exactly one start call (no retry for a quiet-but-healthy service), got %d", svc.startCalls)
	}
	if svc.stopCalls != 0 {
		t.Errorf("expected no stop calls, got %d", svc.stopCalls)
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
