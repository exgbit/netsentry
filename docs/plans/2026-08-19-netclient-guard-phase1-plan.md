# netclient-guard Phase 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 实现 netclient-guard 的核心可测试逻辑——配置一致性判断、backup/watch 决策与执行、诊断信息脱敏与打包——以及一个能在 Windows 上交叉编译运行的最小 CLI(`backup`/`watch`/`diag` 三个子命令)。

**Architecture:** 纯 Go 标准库实现,不引入第三方依赖。业务逻辑拆成 `internal/guardconfig`(解析+一致性判断)、`internal/backup`、`internal/watch`(决策用纯函数 + 通过接口注入的服务控制)、`internal/diag`(脱敏+打包)四个包,均可在任意平台(含本机 macOS)跑单元测试;`internal/winsvc` 是唯一的平台相关代码,用 `//go:build` 分离 Windows 真实实现(shell 出 `sc.exe`)和非 Windows 的占位实现,保证 `go build ./...` 在 macOS 上也能过。

**Tech Stack:** Go 1.21+ 标准库(`encoding/json`、`archive/zip`、`os/exec`、`testing`),不依赖任何第三方模块。

**范围说明:** 本计划**不包含**计划任务注册、Defender 排除、托盘 UI(WebView2)、`setup-netclient` 下载安装 netclient 本体——这些是 Windows-only、需要在真机(已 SSH 打通的 `100.67.147.113`)上手动验证的部分,会在 Phase 1 验证通过后另写 `2026-08-19-netclient-guard-phase2-plan.md`。

---

### Task 0: 项目脚手架

**Files:**
- Create: `go.mod`
- Create: `internal/guardconfig/.gitkeep`(占位,写完 Task 1 后可删除,若目录非空则不需要)

**Step 1: 初始化 go module**

Run:
```bash
cd /Users/justin/work/netclient/.worktrees/netclient-guard
go mod init netclient-guard
```

Expected: 生成 `go.mod`,内容形如:
```
module netclient-guard

go 1.21
```
(实际 go 版本号以 `go version` 输出为准即可,不用手改)

**Step 2: 建目录结构**

Run:
```bash
mkdir -p internal/guardconfig internal/backup internal/watch internal/winsvc internal/diag cmd/netclient-guard
```

**Step 3: Commit**

```bash
git add go.mod
git commit -m "chore: init go module for netclient-guard"
```

---

### Task 1: `guardconfig` 包——解析与一致性判断

**Files:**
- Create: `internal/guardconfig/guardconfig.go`
- Test: `internal/guardconfig/guardconfig_test.go`

**Step 1: 写失败的测试**

Create `internal/guardconfig/guardconfig_test.go`:
```go
package guardconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseNetclientID(t *testing.T) {
	data := []byte(`{"id":"40445c24-cf4d-4653-bf93-1ba975fc5faa","name":"Justin-Win"}`)
	id, err := ParseNetclientID(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "40445c24-cf4d-4653-bf93-1ba975fc5faa" {
		t.Errorf("got id=%q, want 40445c24-cf4d-4653-bf93-1ba975fc5faa", id)
	}
}

func TestParseNetclientID_Empty(t *testing.T) {
	id, err := ParseNetclientID([]byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("got id=%q, want empty string", id)
	}
}

func TestParseServerMQIDs(t *testing.T) {
	data := []byte(`{"tomtoc.cn":{"mqid":"40445c24-cf4d-4653-bf93-1ba975fc5faa","name":"tomtoc.cn"}}`)
	mqids, err := ParseServerMQIDs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mqids["tomtoc.cn"]; got != "40445c24-cf4d-4653-bf93-1ba975fc5faa" {
		t.Errorf("got mqid=%q, want 40445c24-cf4d-4653-bf93-1ba975fc5faa", got)
	}
}

func TestIsConsistent(t *testing.T) {
	cases := []struct {
		name  string
		id    string
		mqids map[string]string
		want  bool
	}{
		{"matching", "abc", map[string]string{"tomtoc.cn": "abc"}, true},
		{"mismatch", "abc", map[string]string{"tomtoc.cn": "xyz"}, false},
		{"no servers", "abc", map[string]string{}, false},
		{"empty id", "", map[string]string{"tomtoc.cn": "abc"}, false},
		{"multiple servers one mismatch", "abc", map[string]string{"a": "abc", "b": "xyz"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsConsistent(c.id, c.mqids); got != c.want {
				t.Errorf("IsConsistent(%q, %v) = %v, want %v", c.id, c.mqids, got, c.want)
			}
		})
	}
}

func TestLoad_BothFilesConsistent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "netclient.json"), `{"id":"abc"}`)
	writeFile(t, filepath.Join(dir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	result, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.NetclientExists || !result.ServersExists {
		t.Fatalf("expected both files to exist, got %+v", result)
	}
	if !result.Consistent {
		t.Errorf("expected consistent, got %+v", result)
	}
}

func TestLoad_Mismatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "netclient.json"), `{"id":"abc"}`)
	writeFile(t, filepath.Join(dir, "servers.json"), `{"tomtoc.cn":{"mqid":"different","name":"tomtoc.cn"}}`)

	result, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Consistent {
		t.Errorf("expected inconsistent, got %+v", result)
	}
}

func TestLoad_NetclientMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "servers.json"), `{"tomtoc.cn":{"mqid":"abc","name":"tomtoc.cn"}}`)

	result, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NetclientExists {
		t.Errorf("expected netclient.json to be reported missing")
	}
	if result.Consistent {
		t.Errorf("expected inconsistent when netclient.json missing")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
```

**Step 2: 运行测试,确认失败**

Run: `go test ./internal/guardconfig/... -v`
Expected: 编译失败(`guardconfig.go` 还不存在,`ParseNetclientID` 等未定义)

**Step 3: 写最小实现**

Create `internal/guardconfig/guardconfig.go`:
```go
// Package guardconfig 负责解析 netclient.json / servers.json 并判断两者的身份 ID 是否一致。
//
// 背景:netclient 启动时会比较 netclient.json 里的 id 字段和 servers.json 里缓存的 mqid 字段,
// 不一致就 fatal 退出且不会自愈(源码见 gravitl/netclient cmd/root.go 的 checkConfig)。
// 这个包把该判断逻辑独立出来,供 backup/watch 复用。
package guardconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ParseNetclientID 从 netclient.json 的原始内容中提取本机身份 ID(id 字段)。
func ParseNetclientID(data []byte) (string, error) {
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("parse netclient.json: %w", err)
	}
	return v.ID, nil
}

// ParseServerMQIDs 从 servers.json 的原始内容中提取每个 server 条目的 name -> mqid 映射。
func ParseServerMQIDs(data []byte) (map[string]string, error) {
	var raw map[string]struct {
		MQID string `json:"mqid"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse servers.json: %w", err)
	}
	result := make(map[string]string, len(raw))
	for key, entry := range raw {
		name := entry.Name
		if name == "" {
			name = key
		}
		result[name] = entry.MQID
	}
	return result, nil
}

// IsConsistent 判断 netclient 的本机身份 ID 与所有已知 server 缓存的 mqid 是否一致。
// 没有任何 server 条目时视为不一致(说明还没加入任何网络,不该当作"健康"状态处理)。
func IsConsistent(netclientID string, mqids map[string]string) bool {
	if netclientID == "" || len(mqids) == 0 {
		return false
	}
	for _, mqid := range mqids {
		if mqid != netclientID {
			return false
		}
	}
	return true
}

// LoadResult 描述从磁盘读取 netclient.json / servers.json 后的状态。
type LoadResult struct {
	NetclientExists bool
	ServersExists   bool
	NetclientID     string
	ServerMQIDs     map[string]string
	Consistent      bool
}

// Load 从指定目录读取 netclient.json 和 servers.json 并做一致性判断。
// dir 通常是 C:\Program Files (x86)\Netclient\,测试时用临时目录替代。
func Load(dir string) (LoadResult, error) {
	var result LoadResult

	ncPath := filepath.Join(dir, "netclient.json")
	if data, err := os.ReadFile(ncPath); err == nil {
		result.NetclientExists = true
		id, err := ParseNetclientID(data)
		if err != nil {
			return result, err
		}
		result.NetclientID = id
	} else if !os.IsNotExist(err) {
		return result, err
	}

	srvPath := filepath.Join(dir, "servers.json")
	if data, err := os.ReadFile(srvPath); err == nil {
		result.ServersExists = true
		mqids, err := ParseServerMQIDs(data)
		if err != nil {
			return result, err
		}
		result.ServerMQIDs = mqids
	} else if !os.IsNotExist(err) {
		return result, err
	}

	result.Consistent = result.NetclientExists && result.ServersExists &&
		IsConsistent(result.NetclientID, result.ServerMQIDs)
	return result, nil
}
```

**Step 4: 运行测试,确认通过**

Run: `go test ./internal/guardconfig/... -v`
Expected: 全部 `PASS`

**Step 5: Commit**

```bash
git add internal/guardconfig
git commit -m "feat: add guardconfig package for id/mqid consistency check"
```

---

### Task 2: `backup` 包

**Files:**
- Create: `internal/backup/backup.go`
- Test: `internal/backup/backup_test.go`

**Step 1: 写失败的测试**

Create `internal/backup/backup_test.go`:
```go
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
```

**Step 2: 运行测试,确认失败**

Run: `go test ./internal/backup/... -v`
Expected: 编译失败(`backup.go` 不存在)

**Step 3: 写最小实现**

Create `internal/backup/backup.go`:
```go
// Package backup 负责在 netclient 配置一致时保存一份"已知良好"快照,供 watch 包在配置损坏时恢复。
package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"netclient-guard/internal/guardconfig"
)

// Outcome 描述一次 backup 执行的结果。
type Outcome int

const (
	OutcomeSkippedMissing Outcome = iota
	OutcomeSkippedInconsistent
	OutcomeSaved
)

func (o Outcome) String() string {
	switch o {
	case OutcomeSkippedMissing:
		return "skipped: netclient.json or servers.json missing"
	case OutcomeSkippedInconsistent:
		return "skipped: currently inconsistent, not overwriting known-good backup"
	case OutcomeSaved:
		return "ok"
	default:
		return "unknown"
	}
}

// Run 检查 netclientDir 下的配置是否一致,一致则把两个文件复制到 backupDir 作为"已知良好"快照。
// 不一致或文件缺失时跳过,绝不用坏状态覆盖已有的良好备份。
func Run(netclientDir, backupDir string) (Outcome, error) {
	load, err := guardconfig.Load(netclientDir)
	if err != nil {
		return OutcomeSkippedMissing, err
	}
	if !load.NetclientExists || !load.ServersExists {
		return OutcomeSkippedMissing, nil
	}
	if !load.Consistent {
		return OutcomeSkippedInconsistent, nil
	}

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return OutcomeSaved, fmt.Errorf("create backup dir: %w", err)
	}
	if err := copyFile(filepath.Join(netclientDir, "netclient.json"), filepath.Join(backupDir, "netclient.json.good")); err != nil {
		return OutcomeSaved, fmt.Errorf("backup netclient.json: %w", err)
	}
	if err := copyFile(filepath.Join(netclientDir, "servers.json"), filepath.Join(backupDir, "servers.json.good")); err != nil {
		return OutcomeSaved, fmt.Errorf("backup servers.json: %w", err)
	}
	stamp := time.Now().Format("2006-01-02 15:04:05")
	if err := os.WriteFile(filepath.Join(backupDir, "last-good.txt"), []byte(stamp), 0o644); err != nil {
		return OutcomeSaved, fmt.Errorf("write last-good.txt: %w", err)
	}
	return OutcomeSaved, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
```

**Step 4: 运行测试,确认通过**

Run: `go test ./internal/backup/... -v`
Expected: 全部 `PASS`

**Step 5: Commit**

```bash
git add internal/backup
git commit -m "feat: add backup package for known-good config snapshots"
```

---

### Task 3: `watch` 包(决策 + 执行)

**Files:**
- Create: `internal/watch/watch.go`
- Test: `internal/watch/watch_test.go`

**Step 1: 写失败的测试**

Create `internal/watch/watch_test.go`:
```go
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
func (f *fakeService) Start() error              { f.startCalls++; f.running = true; return nil }
func (f *fakeService) Stop() error               { f.stopCalls++; f.running = false; return nil }

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
```

**Step 2: 运行测试,确认失败**

Run: `go test ./internal/watch/... -v`
Expected: 编译失败(`watch.go` 不存在)

**Step 3: 写最小实现**

Create `internal/watch/watch.go`:
```go
// Package watch 负责检测 netclient 配置是否损坏、服务是否在运行,并在需要时自动修复。
package watch

import (
	"fmt"
	"os"
	"path/filepath"

	"netclient-guard/internal/guardconfig"
)

// Action 描述 watch 决策后应该采取的动作。
type Action int

const (
	ActionNone Action = iota
	ActionStartService
	ActionRestoreAndStart
	ActionAlertNoBackup
)

func (a Action) String() string {
	switch a {
	case ActionNone:
		return "none"
	case ActionStartService:
		return "start-service"
	case ActionRestoreAndStart:
		return "restore-and-start"
	case ActionAlertNoBackup:
		return "alert-no-backup"
	default:
		return "unknown"
	}
}

// Decide 根据配置一致性、服务是否在运行、是否存在可用的已知良好备份,决定要采取的动作。
func Decide(load guardconfig.LoadResult, serviceRunning, backupAvailable bool) Action {
	if !load.Consistent {
		if backupAvailable {
			return ActionRestoreAndStart
		}
		return ActionAlertNoBackup
	}
	if !serviceRunning {
		return ActionStartService
	}
	return ActionNone
}

// ServiceController 抽象对 netclient Windows 服务的控制,方便测试用假实现替代真实的 Service Control Manager 调用。
type ServiceController interface {
	IsRunning() (bool, error)
	Stop() error
	Start() error
}

// Result 描述一次 watch 执行的结果。
type Result struct {
	Action Action
	Detail string
}

// Run 读取 netclientDir 下的配置,结合服务状态和备份可用性做出决策并执行。
// 返回 error 时(ActionAlertNoBackup)调用方应以非零退出码结束,让计划任务运行历史能反映"修复失败"。
func Run(netclientDir, backupDir string, svc ServiceController) (Result, error) {
	load, err := guardconfig.Load(netclientDir)
	if err != nil {
		return Result{}, err
	}

	goodNC := filepath.Join(backupDir, "netclient.json.good")
	goodSrv := filepath.Join(backupDir, "servers.json.good")
	backupAvailable := fileExists(goodNC) && fileExists(goodSrv)

	running, err := svc.IsRunning()
	if err != nil {
		return Result{}, fmt.Errorf("query service status: %w", err)
	}

	action := Decide(load, running, backupAvailable)

	switch action {
	case ActionNone:
		return Result{Action: action, Detail: "config consistent, service running"}, nil

	case ActionStartService:
		if err := svc.Start(); err != nil {
			return Result{Action: action}, fmt.Errorf("start service: %w", err)
		}
		return Result{Action: action, Detail: "service was not running, started it"}, nil

	case ActionRestoreAndStart:
		if err := svc.Stop(); err != nil {
			return Result{Action: action}, fmt.Errorf("stop service: %w", err)
		}
		if err := copyOverwrite(goodNC, filepath.Join(netclientDir, "netclient.json")); err != nil {
			return Result{Action: action}, fmt.Errorf("restore netclient.json: %w", err)
		}
		if err := copyOverwrite(goodSrv, filepath.Join(netclientDir, "servers.json")); err != nil {
			return Result{Action: action}, fmt.Errorf("restore servers.json: %w", err)
		}
		if err := svc.Start(); err != nil {
			return Result{Action: action}, fmt.Errorf("start service after restore: %w", err)
		}
		return Result{Action: action, Detail: "restored from known-good backup and restarted service"}, nil

	case ActionAlertNoBackup:
		return Result{Action: action}, fmt.Errorf("config invalid and no known-good backup available, manual intervention required")

	default:
		return Result{}, fmt.Errorf("unknown action %v", action)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyOverwrite(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
```

**Step 4: 运行测试,确认通过**

Run: `go test ./internal/watch/... -v`
Expected: 全部 `PASS`

**Step 5: Commit**

```bash
git add internal/watch
git commit -m "feat: add watch package for automatic config repair"
```

---

### Task 4: `winsvc` 包(Windows 服务控制,平台相关)

这个包没法在 macOS 上单元测试(`sc.exe` 只存在于 Windows),所以这一步**不是 TDD**,只做编译校验——两个平台各一份实现,靠 `//go:build` 分离,保证 `go build ./...`/`go vet ./...` 在 macOS 上也能过,交叉编译到 Windows 时才会链接真实实现。

**Files:**
- Create: `internal/winsvc/winsvc_windows.go`
- Create: `internal/winsvc/winsvc_other.go`

**Step 1: 写 Windows 实现**

Create `internal/winsvc/winsvc_windows.go`:
```go
//go:build windows

// Package winsvc 通过 sc.exe 控制 Windows 服务,实现 watch.ServiceController 接口。
// 用 sc.exe 而不是 golang.org/x/sys/windows/svc/mgr,是为了不给这个单文件小工具引入额外依赖。
package winsvc

import (
	"fmt"
	"os/exec"
	"strings"
)

// SCController 通过 sc.exe 控制指定名字的 Windows 服务。
type SCController struct {
	Name string
}

func (c SCController) IsRunning() (bool, error) {
	out, err := exec.Command("sc.exe", "query", c.Name).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("sc query %s: %w: %s", c.Name, err, out)
	}
	return strings.Contains(string(out), "RUNNING"), nil
}

func (c SCController) Start() error {
	out, err := exec.Command("sc.exe", "start", c.Name).CombinedOutput()
	// 1056 = 服务已经在运行,视为成功
	if err != nil && !strings.Contains(string(out), "1056") {
		return fmt.Errorf("sc start %s: %w: %s", c.Name, err, out)
	}
	return nil
}

func (c SCController) Stop() error {
	out, err := exec.Command("sc.exe", "stop", c.Name).CombinedOutput()
	// 1062 = 服务尚未启动,视为成功
	if err != nil && !strings.Contains(string(out), "1062") {
		return fmt.Errorf("sc stop %s: %w: %s", c.Name, err, out)
	}
	return nil
}
```

**Step 2: 写非 Windows 占位实现**

Create `internal/winsvc/winsvc_other.go`:
```go
//go:build !windows

package winsvc

import "errors"

// SCController 在非 Windows 平台上不可用,只是为了让 go build ./... 能跨平台通过。
type SCController struct {
	Name string
}

func (c SCController) IsRunning() (bool, error) {
	return false, errors.New("winsvc: not supported on this platform")
}

func (c SCController) Start() error {
	return errors.New("winsvc: not supported on this platform")
}

func (c SCController) Stop() error {
	return errors.New("winsvc: not supported on this platform")
}
```

**Step 3: 编译校验(两个平台都要过)**

Run:
```bash
go build ./internal/winsvc/...
GOOS=windows GOARCH=amd64 go build ./internal/winsvc/...
```
Expected: 两条命令都无输出、无报错(Go 交叉编译不产生可执行文件是正常的,因为这里只 build 一个包不是 main)

**Step 4: Commit**

```bash
git add internal/winsvc
git commit -m "feat: add winsvc package wrapping sc.exe for Windows service control"
```

---

### Task 5: `diag` 包(脱敏 + 打包)

**Files:**
- Create: `internal/diag/diag.go`
- Test: `internal/diag/diag_test.go`

**Step 1: 写失败的测试**

Create `internal/diag/diag_test.go`:
```go
package diag

import (
	"archive/zip"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeNetclientJSON_StripsSecrets(t *testing.T) {
	input := []byte(`{
		"id": "abc-123",
		"version": "v1.6.0",
		"os": "windows",
		"hostpass": "SUPER-SECRET-PASSWORD",
		"privatekey": [1,2,3,4],
		"traffickeyprivate": "base64secret",
		"publickey": [5,6,7,8]
	}`)

	out, err := SanitizeNetclientJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "SUPER-SECRET-PASSWORD") {
		t.Errorf("sanitized output must not contain hostpass value, got: %s", s)
	}
	if strings.Contains(s, "hostpass") || strings.Contains(s, "privatekey") ||
		strings.Contains(s, "traffickeyprivate") || strings.Contains(s, "publickey") {
		t.Errorf("sanitized output must not contain secret field names, got: %s", s)
	}
	if !strings.Contains(s, "abc-123") {
		t.Errorf("sanitized output should keep the id field, got: %s", s)
	}
}

func TestSanitizeServersJSON_StripsSecrets(t *testing.T) {
	input := []byte(`{
		"tomtoc.cn": {
			"mqid": "abc-123",
			"name": "tomtoc.cn",
			"accesskey": "SUPER-SECRET-KEY"
		}
	}`)

	out, err := SanitizeServersJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "SUPER-SECRET-KEY") || strings.Contains(s, "accesskey") {
		t.Errorf("sanitized output must not contain accesskey, got: %s", s)
	}
	if !strings.Contains(s, "abc-123") {
		t.Errorf("sanitized output should keep the mqid value, got: %s", s)
	}
}

func TestBundle_WritesReadableZip(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "diag.zip")
	sources := []Source{
		{Name: "guard.log", Data: []byte("hello log")},
		{Name: "config-summary/netclient.json", Data: []byte(`{"id":"abc"}`)},
	}

	if err := Bundle(sources, outPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("failed to open produced zip: %v", err)
	}
	defer r.Close()

	got := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", f.Name, err)
		}
		got[f.Name] = string(data)
	}

	if got["guard.log"] != "hello log" {
		t.Errorf("guard.log content mismatch, got %q", got["guard.log"])
	}
	if got["config-summary/netclient.json"] != `{"id":"abc"}` {
		t.Errorf("netclient.json content mismatch, got %q", got["config-summary/netclient.json"])
	}
}
```

**Step 2: 运行测试,确认失败**

Run: `go test ./internal/diag/... -v`
Expected: 编译失败(`diag.go` 不存在)

**Step 3: 写最小实现**

Create `internal/diag/diag.go`:
```go
// Package diag 负责生成可以安全对外分享的诊断包:脱敏敏感字段后打包成 zip。
package diag

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
)

var allowedNetclientFields = map[string]bool{
	"id": true, "version": true, "os": true, "os_version": true,
	"interface": true, "name": true, "nodes": true, "endpointip": true,
	"created_at": true, "updated_at": true,
}

// SanitizeNetclientJSON 从 netclient.json 原始内容中只保留白名单字段,剔除私钥/密码等敏感信息。
// 用白名单而不是黑名单,是为了防止未来 netclient 新增未知的敏感字段时被漏掉。
func SanitizeNetclientJSON(data []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse netclient.json: %w", err)
	}
	clean := make(map[string]json.RawMessage)
	for k, v := range raw {
		if allowedNetclientFields[k] {
			clean[k] = v
		}
	}
	return json.MarshalIndent(clean, "", "  ")
}

var allowedServerFields = map[string]bool{
	"mqid": true, "name": true,
}

// SanitizeServersJSON 对 servers.json 里每个 server 条目做同样的白名单过滤。
func SanitizeServersJSON(data []byte) ([]byte, error) {
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse servers.json: %w", err)
	}
	clean := make(map[string]map[string]json.RawMessage, len(raw))
	for server, fields := range raw {
		cleanFields := make(map[string]json.RawMessage)
		for k, v := range fields {
			if allowedServerFields[k] {
				cleanFields[k] = v
			}
		}
		clean[server] = cleanFields
	}
	return json.MarshalIndent(clean, "", "  ")
}

// Source 是要写入诊断 zip 的一个文件条目。
type Source struct {
	Name string // zip 内的路径,比如 "guard.log" 或 "config-summary/netclient.json"
	Data []byte
}

// Bundle 把一组已经收集好、已脱敏的诊断内容打包成一个 zip 文件。
// 调用方负责在传入前完成脱敏(SanitizeNetclientJSON/SanitizeServersJSON)和真实系统状态的采集。
func Bundle(sources []Source, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create diag zip: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, s := range sources {
		w, err := zw.Create(s.Name)
		if err != nil {
			return fmt.Errorf("add %s to zip: %w", s.Name, err)
		}
		if _, err := w.Write(s.Data); err != nil {
			return fmt.Errorf("write %s to zip: %w", s.Name, err)
		}
	}
	return zw.Close()
}
```

**Step 4: 运行测试,确认通过**

Run: `go test ./internal/diag/... -v`
Expected: 全部 `PASS`

**Step 5: Commit**

```bash
git add internal/diag
git commit -m "feat: add diag package for sanitized diagnostic bundles"
```

---

### Task 6: 最小 CLI 接线 + 交叉编译校验

**Files:**
- Create: `cmd/netclient-guard/main.go`

**Step 1: 写 main.go**

Create `cmd/netclient-guard/main.go`:
```go
// netclient-guard 是一个 Windows 后台工具,用于自动备份/恢复 netclient 的身份配置,
// 修复因 netclient.json 与 servers.json 不一致导致的启动崩溃(已知的 netclient 自愈缺陷)。
//
// Phase 1 只实现 backup/watch/diag 三个无 UI 子命令;计划任务注册、Defender 排除、
// 托盘 UI、netclient 安装/加入网络留给 Phase 2。
package main

import (
	"fmt"
	"os"

	"netclient-guard/internal/backup"
	"netclient-guard/internal/diag"
	"netclient-guard/internal/watch"
	"netclient-guard/internal/winsvc"
)

const (
	netclientDir = `C:\Program Files (x86)\Netclient\`
	guardDir     = `C:\ProgramData\netclient-guard\`
)

func backupDir() string { return guardDir + `backup\` }

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: netclient-guard <backup|watch|diag>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "backup":
		runBackup()
	case "watch":
		runWatch()
	case "diag":
		runDiag()
	default:
		fmt.Println("unknown command:", os.Args[1])
		os.Exit(1)
	}
}

func runBackup() {
	outcome, err := backup.Run(netclientDir, backupDir())
	if err != nil {
		fmt.Println("backup error:", err)
		os.Exit(1)
	}
	fmt.Println("backup:", outcome)
}

func runWatch() {
	svc := winsvc.SCController{Name: "netclient"}
	result, err := watch.Run(netclientDir, backupDir(), svc)
	if err != nil {
		fmt.Println("watch ALERT:", err)
		os.Exit(1)
	}
	fmt.Println("watch:", result.Action, "-", result.Detail)
}

func runDiag() {
	// Phase 1 最小可用版本:只打包脱敏后的配置。
	// winsw 日志、guard.log、服务状态、计划任务历史、Defender 状态留给 Phase 2
	// (这些采集逻辑本来就要跟 Phase 2 的计划任务/Defender 代码写在一起)。
	ncData, err := os.ReadFile(netclientDir + "netclient.json")
	if err != nil {
		fmt.Println("diag error reading netclient.json:", err)
		os.Exit(1)
	}
	srvData, err := os.ReadFile(netclientDir + "servers.json")
	if err != nil {
		fmt.Println("diag error reading servers.json:", err)
		os.Exit(1)
	}
	cleanNC, err := diag.SanitizeNetclientJSON(ncData)
	if err != nil {
		fmt.Println("diag error sanitizing netclient.json:", err)
		os.Exit(1)
	}
	cleanSrv, err := diag.SanitizeServersJSON(srvData)
	if err != nil {
		fmt.Println("diag error sanitizing servers.json:", err)
		os.Exit(1)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("diag error resolving home directory:", err)
		os.Exit(1)
	}
	outPath := home + `\Desktop\netclient-diag.zip`
	err = diag.Bundle([]diag.Source{
		{Name: "config-summary/netclient.json", Data: cleanNC},
		{Name: "config-summary/servers.json", Data: cleanSrv},
	}, outPath)
	if err != nil {
		fmt.Println("diag error writing bundle:", err)
		os.Exit(1)
	}
	fmt.Println("diag bundle written to", outPath)
}
```

**Step 2: 本机(macOS)编译与测试全量校验**

Run:
```bash
go build ./...
go vet ./...
go test ./...
```
Expected: 三条命令都无报错;`go test` 输出全部包 `ok`

**Step 3: 交叉编译到 Windows,确认能产出真实目标平台的可执行文件**

Run:
```bash
GOOS=windows GOARCH=amd64 go build -o /tmp/netclient-guard.exe ./cmd/netclient-guard
file /tmp/netclient-guard.exe
```
Expected: `file` 命令输出包含 `PE32+ executable ... for MS Windows`(证明产出的是合法的 Windows 可执行文件;不要求在 macOS 上运行它)

**Step 4: Commit**

```bash
git add cmd/netclient-guard
git commit -m "feat: wire up backup/watch/diag subcommands in CLI entrypoint"
```

---

## Phase 1 完成后的验证清单

在真实 Windows 机器(`100.67.147.113`,已有 SSH 访问)上手动验证(把 Task 6 产出的 `.exe` scp 过去):

1. `netclient-guard.exe backup` 跑一次,确认 `C:\ProgramData\netclient-guard\backup\` 下生成了 `.good` 文件,内容和当前 `netclient.json`/`servers.json` 一致
2. 手动删掉真机上的 `netclient.json`(先用今天已有的 PowerShell 备份留一份底,避免真出问题没法恢复),跑 `netclient-guard.exe watch`,确认它能从 `.good` 文件恢复并重启 netclient 服务
3. 确认 `diag` 生成的 zip 里没有任何私钥/密码字段

验证通过后再开始写 Phase 2 计划(计划任务注册、Defender 排除、托盘 UI、`setup-netclient`)。
