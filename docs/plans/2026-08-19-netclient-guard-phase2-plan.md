# netclient-guard Phase 2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans / superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** 在 Phase 1(`guardconfig`/`backup`/`watch`/`winsvc`/`diag` + 最小 CLI,已合并到 `main`)基础上,补齐设计文档(`docs/plans/2026-08-19-netclient-guard-design.md`)里的剩余能力:`install`/`uninstall`(计划任务注册 + Defender 排除 + 托盘开机启动)、持久化日志、`diag` 包扩展到完整信息集、`setup-netclient`(下载安装 netclient 本体并加入网络)、托盘 UI。

**Architecture:** 延续 Phase 1"纯逻辑用标准库 TDD、平台相关代码用 `//go:build` 分离 + 真机手动验证"的模式。新增包全部放在 `internal/` 下,每个包尽量拆成"纯函数(命令行参数构造、XML/文本模板)"+"薄的 OS 调用包装(shell 到 `schtasks.exe`/`powershell.exe`/`reg.exe`)"两层,前者 TDD,后者靠真机验证。

**Tech Stack:** 与 Phase 1 一致,标准库为主。托盘 UI 需要引入两个新依赖(`github.com/getlantern/systray`、WebView2 绑定库)——**具体选型待 Task 0 的调研结果确认**,见下文。

**范围**:Phase 1 已验证的 `backup`/`watch`/`winsvc`/`diag`/`guardconfig` 不动,只做设计文档里点名要做但 Phase 1 明确排除的部分。

---

## Task 0(前置调研,已完成,结论如下)

托盘图标(`getlantern/systray`)和 WebView2 面板绑定库在 Windows 上是否需要 CGO,直接决定 Task 9(托盘 UI)能不能沿用"Mac 上 `GOOS=windows go build` 交叉编译、真机测试"这套已经跑通两轮的工作流。

**调研结论(逐文件核对源码,非猜测):**
- `github.com/getlantern/systray`:Windows 实现(`systray_windows.go`)纯 Go,全部通过 `golang.org/x/sys/windows` + `syscall` 调 Win32 API,无 `import "C"`。
- WebView2 绑定选 `github.com/jchv/go-webview2`(不是 `github.com/webview/webview_go`——后者必须 CGO,已排除):逐文件核对 `webview.go`/`pkg/edge/*`/`internal/w32/*` 及其依赖 `github.com/jchv/go-winloader`,全部纯 Go,自己实现了一个内存 PE 加载器来动态加载 `WebView2Loader.dll`,不需要 CGO。

**结论:两个依赖都能 `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build` 直接从这台 Mac 交叉编译,不需要装 mingw-w64,不需要换开发方式。** 唯一的运行时前提(不影响编译):目标机器要有 Microsoft Edge WebView2 Runtime,Win11 默认自带。

Task 1-8 与托盘 UI 无关,已经在下面按此结论排好序,可以直接开工。

---

## Task 1: `internal/guardlog` 包——持久化日志

**背景**:Phase 1 的 `backup`/`watch`/`diag` 子命令目前只 `fmt.Println` 到 stdout——计划任务跑起来这些输出会直接丢失,设计文档要求的 `guard.log`(供 `diag` 打包、供人工排查)现在实际上不存在。这是 Phase 1 遗留的一个真实缺口,必须先补上,后面几个 Task 都要用。

**Files:**
- Create: `internal/guardlog/guardlog.go`
- Test: `internal/guardlog/guardlog_test.go`

**API:**
```go
// Package guardlog 提供一个极简的追加写入日志,格式:[时间戳] [tag] message
package guardlog

// Append 把一行日志追加写入 path,自动创建文件(如果不存在)。
func Append(path, tag, message string) error
```

**TDD 要求:**
- 测试用 `t.TempDir()` 里的一个不存在的文件路径调用 `Append`,验证文件被创建且内容包含 tag 和 message
- 多次调用 `Append`,验证是追加而不是覆盖(第二次调用后文件里两行都在)
- 验证每行格式大致为 `[YYYY-MM-DD HH:MM:SS] [tag] message`(时间戳具体值没法断言,断言结构:以 `[` 开头、包含 `] [tag] message`)

**实现要点**:`os.OpenFile` 用 `os.O_APPEND|os.O_CREATE|os.O_WRONLY`,`time.Now().Format("2006-01-02 15:04:05")`。不需要加锁/并发保护——4 个计划任务都设置了 `MultipleInstancesPolicy=IgnoreNew`(见 Task 2),同一时刻不会有两个 `backup`/`watch` 进程同时写。

**Step: Commit** `feat: add guardlog package for persistent logging`

---

## Task 2: `internal/schedtask` 包——计划任务注册(纯函数部分)

**Files:**
- Create: `internal/schedtask/schedtask.go`
- Test: `internal/schedtask/schedtask_test.go`

**先做纯函数、可测试的部分:**

```go
package schedtask

// BackupTaskArgs 构造 schtasks /Create 用来注册"每 30 分钟跑一次 backup"任务的参数。
func BackupTaskArgs(exePath string) []string {
	return []string{
		"/Create", "/TN", "NetclientGuardBackup",
		"/TR", `"` + exePath + `" backup`,
		"/SC", "MINUTE", "/MO", "30",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
}

// WatchTaskArgs 构造"每 5 分钟跑一次 watch"任务的参数。
func WatchTaskArgs(exePath string) []string {
	return []string{
		"/Create", "/TN", "NetclientGuardWatch",
		"/TR", `"` + exePath + `" watch`,
		"/SC", "MINUTE", "/MO", "5",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
}

// WatchOnStartTaskArgs 构造"开机 1 分钟后跑一次 watch"任务的参数。
func WatchOnStartTaskArgs(exePath string) []string {
	return []string{
		"/Create", "/TN", "NetclientGuardWatchOnStart",
		"/TR", `"` + exePath + `" watch`,
		"/SC", "ONSTART", "/DELAY", "0001:00",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/F",
	}
}

// ResumeTriggerTaskXML 生成"系统从睡眠唤醒时跑一次 watch"任务的 XML 任务定义
// (schtasks 的 /SC 系列 flag 表达不了事件触发器,只能用 /XML 传任务定义文件)。
// 监听 System 日志里 Microsoft-Windows-Power-Troubleshooter 来源、EventID=1 的事件
// (对应"系统从睡眠恢复")。RunLevel 用 SID S-1-5-18(Local System 的固定 SID,不受
// 系统语言/本地化影响)而不是 "NT AUTHORITY\SYSTEM" 这种可本地化的名字。
func ResumeTriggerTaskXML(exePath string) string {
	...
}

// AllTaskNames 返回本工具注册的全部计划任务名,uninstall 时用来逐个删除。
func AllTaskNames() []string {
	return []string{
		"NetclientGuardBackup",
		"NetclientGuardWatch",
		"NetclientGuardWatchOnStart",
		"NetclientGuardWatchOnResume",
	}
}
```

**TDD 要求:**
- `BackupTaskArgs`/`WatchTaskArgs`/`WatchOnStartTaskArgs`:给定一个示例 `exePath`(比如 `C:\ProgramData\netclient-guard\netclient-guard.exe`),断言返回的 `[]string` 与预期完全一致(逐元素比较)
- `ResumeTriggerTaskXML`:
  - 用 `encoding/xml` 把返回的字符串 `Unmarshal` 进一个匿名/最小结构体,确认是合法 XML(不会报错)
  - 断言返回内容包含 `exePath` 参数值(在 `<Command>` 里)
  - 断言包含 `"Power-Troubleshooter"` 和 `"EventID=1"`(或等价的查询条件文本)
- `AllTaskNames`:断言返回长度为 4,且包含上面 4 个具体任务名

**Step: Commit** `feat: add schedtask package for scheduled task argument/XML construction (pure functions)`

---

## Task 3: `internal/schedtask` 包——实际注册/注销(薄封装,Windows-only)

**Files:**
- Create: `internal/schedtask/register_windows.go`(`//go:build windows`)
- Create: `internal/schedtask/register_other.go`(`//go:build !windows`)

**API(两个平台签名一致):**
```go
// Register 注册全部 4 个计划任务(exePath 是已安装到位的 netclient-guard.exe 路径)。
// 幂等:每次都用 /F 强制覆盖,重复调用安全。
func Register(exePath string) error

// Unregister 删除全部 4 个计划任务。任务本来就不存在时不视为错误
// (schtasks /Delete 对不存在的任务返回非零但我们应当忽略这种情况,uninstall 要能在
// 部分安装/重复卸载的情况下正常跑完,不半途而废)。
func Unregister() error
```

**Windows 实现要点:**
- 前 3 个任务:`exec.Command("schtasks.exe", schedtask.BackupTaskArgs(exePath)...)`(用 Task 2 的纯函数构造参数,这里只负责执行 + 收集报错)
- 第 4 个任务(`NetclientGuardWatchOnResume`):把 `schedtask.ResumeTriggerTaskXML(exePath)` 写到一个临时文件(`os.CreateTemp`),再 `schtasks.exe /Create /TN NetclientGuardWatchOnResume /XML <tmpfile> /RU SYSTEM /F`,执行完删除临时文件
- 任一任务注册失败,记录到位错误里继续尝试剩下的任务,最后把所有失败原因合并返回(不要第一个任务失败就直接放弃,应该让 `install` 尽量把能装的都装上,失败的部分体现在 `install.log` 里)
- `Unregister`:遍历 `schedtask.AllTaskNames()`,逐个 `schtasks.exe /Delete /TN <name> /F`,忽略"任务不存在"这类错误(参考 Phase 1 `winsvc` 里对 sc.exe 1056/1062 错误码的处理方式,这里判断 `schtasks` 的"找不到指定的计划任务"这类输出/错误码,具体错误码请在真机上跑一次 `schtasks /Delete /TN 一个不存在的任务名 /F` 确认,不要凭空猜测)

**非 Windows 桩实现:** 和 `winsvc_other.go` 一样返回 "not supported on this platform"。

**验证**:无法单测(依赖真实 `schtasks.exe`),编译校验(`go build ./...` 原生 + `GOOS=windows GOARCH=amd64`)+ 真机手动验证(见 Task 6 之后的整体验证清单)。

**Step: Commit** `feat: add schedtask Windows registration via schtasks.exe`

---

## Task 4: `internal/defenderexcl` 包——Defender 排除项(薄封装,Windows-only)

**Files:**
- Create: `internal/defenderexcl/defenderexcl_windows.go`(`//go:build windows`)
- Create: `internal/defenderexcl/defenderexcl_other.go`(`//go:build !windows`)

**API:**
```go
// Add 把 path 加入 Windows Defender 排除列表。已经在排除列表里时重复调用不报错。
func Add(path string) error

// Remove 从排除列表移除 path。本来就不在列表里时不视为错误。
func Remove(path string) error
```

**Windows 实现**:分别 shell 到
```
powershell.exe -NoProfile -Command "Add-MpPreference -ExclusionPath '<path>'"
powershell.exe -NoProfile -Command "Remove-MpPreference -ExclusionPath '<path>'"
```
(`Add-MpPreference`/`Remove-MpPreference` 本身对重复添加/删除是幂等的,不用额外查重)

**非 Windows 桩实现**:同上模式。

**设计文档要求**:`install` 时这一步失败(比如公司用组策略统一管理杀毒软件、`Add-MpPreference` 被拒绝)只记警告,不阻断安装其余步骤——这个"失败不阻断"的处理放在 Task 6(`install` 整体流程),这里的 `Add`/`Remove` 函数本身老老实实返回 error 就行,不用自己吞掉。

**验证**:无法单测,编译校验 + 真机验证(需要一台没有组策略限制的机器确认能成功加,以及故意在企业策略环境下测试失败不阻断——如果没有这样的测试环境,至少验证成功路径,失败路径靠 code review 确认逻辑正确)。

**Step: Commit** `feat: add defenderexcl package for Windows Defender exclusion management`

---

## Task 5: `internal/autostart` 包——托盘开机启动项(薄封装 + 纯函数,Windows-only)

**Files:**
- Create: `internal/autostart/autostart.go`(纯函数部分,跨平台可测)
- Create: `internal/autostart/autostart_windows.go`(`//go:build windows`)
- Create: `internal/autostart/autostart_other.go`(`//go:build !windows`)

**纯函数部分(`autostart.go`,可测):**
```go
package autostart

const runValueName = "NetclientGuardTray"

// RegisterArgs 构造把 tray 加进当前用户登录启动项的 reg.exe 参数
// (HKCU\Software\Microsoft\Windows\CurrentVersion\Run,不需要管理员权限的那个键,
// 区别于 HKLM 下同名的、需要管理员权限的启动项)。
func RegisterArgs(exePath string) []string {
	return []string{
		"add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", runValueName, "/t", "REG_SZ",
		"/d", `"` + exePath + `" tray`, "/f",
	}
}

// UnregisterArgs 构造删除该启动项的 reg.exe 参数。
func UnregisterArgs() []string {
	return []string{
		"delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", runValueName, "/f",
	}
}
```

**TDD**:两个函数各自断言返回值与预期 `[]string` 完全一致,给一个示例 `exePath`。

**薄封装(Windows-only,shell 到 `reg.exe`):**
```go
func Register(exePath string) error   // exec.Command("reg.exe", autostart.RegisterArgs(exePath)...)
func Unregister() error               // exec.Command("reg.exe", autostart.UnregisterArgs()...)
```
`Unregister` 对"值不存在"的错误要忽略(参考前面几个包处理"删除不存在的东西"的一致模式)。

**重要**:这个包操作的是 `HKCU`(当前用户),而 `install`/`uninstall` 整体是以管理员权限运行的(见 Task 6)——管理员提权后的进程如果是通过 UAC 弹窗提权,`HKCU` 通常仍然指向发起提权的那个用户,不会错误地写到 `SYSTEM` 或其他用户的 profile 下;但如果 `install.exe` 是通过计划任务/远程工具以 `SYSTEM` 身份运行的(不是本设计的预期用法,但要留意),`HKCU` 会指向 `SYSTEM` 账户而不是实际用户,导致这个启动项形同虚设。**这个风险点在真机验证时要专门确认**:用普通用户账号触发 UAC 提权跑 `install`,重新登录后确认 tray 真的自动启动了。

**Step: Commit** `feat: add autostart package for tray login startup registration`

---

## Task 6: `install`/`uninstall` 子命令——整体接线

**Files:**
- Modify: `cmd/netclient-guard/main.go`(新增 `install`/`uninstall` 分支 + 辅助函数)
- Create: `internal/elevate/elevate_windows.go` + `elevate_other.go`(管理员权限检测 + UAC 自提权重启)

**`internal/elevate` API:**
```go
// IsElevated 判断当前进程是否以管理员权限运行。
func IsElevated() (bool, error)

// RelaunchElevated 用 UAC 提权重新启动当前程序(带上同样的命令行参数),
// 成功发起后调用方应该直接退出当前(非提权的)进程。
func RelaunchElevated(args []string) error
```

Windows 实现:
- `IsElevated`:shell `net session` 并检查退出码(admin 权限下成功、非 admin 下会报"系统错误 5:拒绝访问"并返回非零)——这是一个广泛使用的、不需要额外依赖的管理员权限检测技巧,真机验证时确认这个判断在当前 Windows 11 版本上依然准确
- `RelaunchElevated`:`powershell.exe -Command "Start-Process -FilePath '<自身可执行文件路径>' -ArgumentList '<args>' -Verb RunAs -Wait"`

**`install` 命令流程(`runInstall()` in main.go):**
1. `elevate.IsElevated()`,不是管理员就 `elevate.RelaunchElevated(os.Args[1:])` 然后退出(退出码 0,因为提权后的子进程会接管剩下的工作,当前进程只是个跳板)
2. 把自身(`os.Executable()`)复制到 `C:\ProgramData\netclient-guard\netclient-guard.exe`(如果当前已经就是从这个路径运行,跳过复制)
3. `schedtask.Register(installedExePath)` —— 失败只记 `guardlog.Append(installLog, "WARN", ...)`,不中止
4. `defenderexcl.Add(netclientDir)` —— 同上,失败只记警告
5. 如果 `C:\Program Files (x86)\Netclient\netclient.json` 已存在(netclient 已装好且已 join 过),立即跑一次相当于 `backup` 子命令的逻辑,建立基线
6. `autostart.Register(installedExePath)`
7. 每一步的成功/失败都写入 `C:\ProgramData\netclient-guard\install.log`(用 Task 1 的 `guardlog.Append`)
8. 全部完成后打印一句人类可读的总结(装了几项、有没有警告),而不是像 `backup`/`watch` 那样只给一行

**`uninstall` 命令流程(`runUninstall()`,同样先做 `elevate` 检查):**
1. `schedtask.Unregister()`
2. `defenderexcl.Remove(netclientDir)`
3. `autostart.Unregister()`
4. 默认保留 `C:\ProgramData\netclient-guard\backup\` 下的历史备份;加了 `--purge` 参数(检查 `os.Args` 里有没有这个 flag)才连 `C:\ProgramData\netclient-guard\` 整个目录一起删

**验证**:无法单测这层接线(全是真实系统调用的编排),`go build`/`go vet`/交叉编译校验 + 真机手动验证(见文末验证清单)。

**Step: Commit** `feat: wire up install/uninstall subcommands`

---

## Task 7: `diag` 扩展到完整信息集

设计文档要求 `diag` 收集 7 类信息,Phase 1 只做了第 3 类(脱敏后的 config)。这个 Task 补齐剩下 6 类,并把输出文件名改成带时间戳(`netclient-diag-<时间戳>.zip`,之前 Phase 1 是固定文件名会覆盖旧包)。

**Files:**
- Modify: `cmd/netclient-guard/main.go` 的 `runDiag()`
- Create: `internal/sysreport/sysreport_windows.go` + `sysreport_other.go` —— 新增几个"跑个命令/读个文件、原样返回文本"的采集函数

**`internal/sysreport` API(Windows-only,薄封装,不需要 TDD,和 winsvc 的 `sc.exe` 封装一个性质):**
```go
func ServiceStatus() (string, error)      // sc.exe query netclient 的输出 + winsw.xml 文件内容拼在一起
func ScheduledTasksStatus() (string, error) // 对 schedtask.AllTaskNames() 逐个跑 schtasks /Query /TN <name> /FO LIST /V,拼起来
func DefenderStatus() (string, error)     // powershell Get-MpPreference 的排除列表 + Get-MpThreatDetection 过滤出路径含 "Netclient" 的条目
func SystemInfo(guardVersion string) (string, error) // Windows 版本 + netclient 版本(读 netclient.json 的 version 字段)+ guard 版本 + hostname + 生成时间
```

**`runDiag()` 改动:**
1. 输出路径改成 `home + \Desktop\netclient-diag-<time.Now().Format("20060102-150405")>.zip`
2. 除了 Phase 1 已有的 `config-summary/netclient.json`/`servers.json`,追加以下 `diag.Source`:
   - 直接读 `netclientDir + "logs\winsw.out.log"` / `winsw.err.log` 原样收录(读不到就跳过、不算整个 diag 失败——这两个文件缺失不该阻止拿到其余信息)
   - 直接读 `guardDir + "guard.log"`(同上,读不到就跳过)
   - `sysreport.ServiceStatus()` → `Source{Name: "service-status.txt", ...}`
   - `sysreport.ScheduledTasksStatus()` → `Source{Name: "scheduled-tasks.txt", ...}`
   - `sysreport.DefenderStatus()` → `Source{Name: "defender-status.txt", ...}`
   - `sysreport.SystemInfo(guardVersion)` → `Source{Name: "system-info.txt", ...}`
   - 上面 4 个 `sysreport` 调用任一失败,把错误信息本身写进对应的 txt 文件内容里(而不是让整个 `diag` 命令失败退出)——诊断包的价值在于"能采集多少算多少",一个来源采集失败不该让用户拿不到其余有用信息

**验证**:`go build`/`go vet`/交叉编译 + 真机验证(生成一个真实 diag 包,人工解压检查 7 类文件都在、`config-summary` 部分依然没有敏感字段——Phase 1 已经验证过脱敏逻辑本身没问题,这里只需确认新增的 6 类信息没有意外夹带敏感内容,比如 `winsw.xml`/`schtasks /Query` 的输出理论上不含密钥,但要实际看一眼确认)。

**Step: Commit** `feat: expand diag bundle to full 7-source information set`

---

## Task 8: `setup-netclient` 子命令

**Files:**
- Create: `internal/netclientinstall/netclientinstall.go`(纯函数部分)
- Create: `internal/netclientinstall/download_windows.go` + `download_other.go`(薄封装)
- Modify: `cmd/netclient-guard/main.go`(新增 `setup-netclient` 分支)

**纯函数部分(可测):**
```go
package netclientinstall

// PinnedVersion 是团队验证过的稳定 netclient 版本,编译时常量,升级 = 改这个值重新编译发布。
const PinnedVersion = "v1.6.0"

// DownloadURL 构造指定版本 netclient Windows 二进制的 GitHub Releases 下载地址。
func DownloadURL(version string) string {
	return fmt.Sprintf("https://github.com/gravitl/netclient/releases/download/%s/netclient-windows-amd64.exe", version)
}
```

**TDD**:`DownloadURL("v1.6.0")` 断言返回值与预期字符串完全一致(硬编码断言,不用正则)。

**薄封装(Windows-only):**
```go
// Run 执行完整的"下载 → 安装 → 加入网络 → 联动装 guard"流程。
func Run(token string) error
```
内部步骤(严格按已经从 `gravitl/netclient` 源码核实过的真实命令序列,不要自己发明):
1. `http.Get(netclientinstall.DownloadURL(netclientinstall.PinnedVersion))`,写到一个临时文件,`chmod` 可执行(Windows 上其实不需要,但保险起见设一下)
2. `exec.Command(临时路径, "install").Run()` —— 这一步会让 netclient 自己把自身拷到 `C:\Program Files (x86)\Netclient\` 并注册 WinSW 服务(这是 netclient 自己的 `functions.Install()` 逻辑,我们不用管细节,只管调用)
3. `exec.Command(`C:\Program Files (x86)\Netclient\netclient.exe`, "join", "-t", token).Run()`
4. 前两步任一失败立即返回 error,带上是哪一步失败的上下文(下载/安装/加入网络分别报不同的错误前缀,方便面板/CLI 显示明确的失败阶段)
5. 全部成功后,调用当前 `netclient-guard.exe` 自身的 `install` 逻辑(即 Task 6 的 `runInstall()` 那套步骤——注意这一步不需要重新走 `elevate` 检查,因为 `setup-netclient` 本身运行时已经是管理员权限,直接复用 `runInstall` 内部逻辑即可,不要再触发一次 UAC 提权)
6. 跑一次 `backup` 建立基线

**CLI 接线**:`netclient-guard.exe setup-netclient -t <token>`,同样需要 `elevate` 检查(不是管理员就 UAC 提权重启自己,带上 `-t` 参数)。

**验证**:这个 Task 需要一个真实的 enrollment token 才能端到端验证(从 Netmaker 服务器 UI 生成)——**真机验证前需要你(用户)提供一个测试用的 enrollment token**,或者指定一台可以用来测试全新加入网络的机器/网络,避免误加入生产网络。如果暂时没有测试环境,这个 Task 的代码本身按计划实现、通过编译校验和 code review,标记"端到端未验证",后续有测试条件了再补验证。

**Step: Commit** `feat: add setup-netclient subcommand for netclient installation and network join`

---

## Task 9: 托盘 UI

沿用本机 Mac 交叉编译的开发方式(Task 0 已确认)。这个 Task 本质是"手工搭一个能跑的 Windows GUI 程序,反复传真机跑起来看效果",不是传统意义上能红-绿-提交的 TDD——真正可以单测的只有"读取当前状态、拼成给面板用的数据结构"这一小块纯逻辑,其余(systray 生命周期、WebView2 渲染、JS bridge)只能靠真机跑起来肉眼验证。拆成三个子任务:

### 9a:`internal/trayui` 状态聚合(可测的部分)

**Files:**
- Create: `internal/trayui/status.go`
- Test: `internal/trayui/status_test.go`

```go
package trayui

// Status 是面板/图标需要的全部信息,序列化成 JSON 传给前端。
type Status struct {
	Configured    bool   `json:"configured"`    // netclient.json 是否存在(决定面板显示"首次配置表单"还是"仪表盘")
	Healthy       bool   `json:"healthy"`        // 配置一致 且 服务 Running(决定图标绿/红)
	ServerName    string `json:"serverName"`
	LastBackup    string `json:"lastBackup"`     // 读 backup\last-good.txt,读不到就空字符串
	ServiceStatus string `json:"serviceStatus"`  // "Running"/"Stopped"/"Unknown"
}

// Collect 聚合 guardconfig.Load + 服务状态 + last-good.txt,得到当前状态。
// svc 是 watch.ServiceController(复用 Phase 1 已有接口,不新造一个),方便测试用假实现。
func Collect(netclientDir, backupDir string, svc interface{ IsRunning() (bool, error) }) (Status, error)
```

**TDD 要求**(和 Phase 1 `watch`/`backup` 包一个套路,`t.TempDir()` + 假 service):
- 未检测到 `netclient.json` → `Configured: false`
- 配置一致 + 服务 Running → `Healthy: true`
- 配置不一致,或服务非 Running → `Healthy: false`
- `last-good.txt` 存在 → `LastBackup` 是文件内容;不存在 → 空字符串

**Step: Commit** `feat: add trayui status aggregation package`

### 9b:systray 图标 + 精简原生右键菜单

**Files:**
- Modify: `cmd/netclient-guard/main.go`(新增 `tray` 分支)
- Create: `internal/trayui/icon.go`(嵌入图标资源)

**要点:**
- 用 `github.com/getlantern/systray` 的 `systray.Run(onReady, onExit)`
- 需要两个小图标(绿色圆点/红色圆点,16x16 或 32x32 `.ico`)。这两个图标文件不在这份文档里写死字节内容——实现时用任意简单方式生成(比如写一个一次性小脚本用 `image`/`image/color` 画个实心圆再编码成 ico,或者找一个现成的纯色圆点图标),存成 `internal/trayui/assets/green.ico`、`internal/trayui/assets/red.ico`,用 `//go:embed assets/*.ico` 嵌入二进制,不引入运行时对外部文件的依赖
- `onReady`:调用 `trayui.Collect(...)` 决定初始图标,起一个 `time.Ticker`(30 秒)在后台循环刷新图标(对应设计文档"图标状态每 30 秒刷新一次");注册左键点击回调(弹出 9c 的面板)和右键菜单(`退出`、`重启托盘`——`重启托盘`就是 `os.StartProcess` 自己再拉起一个 `tray` 进程后退出当前进程,处理面板卡死等极端情况的兜底手段)

**验证**:交叉编译后 scp 到真机跑 `netclient-guard.exe tray`,肉眼确认托盘图标出现、颜色随状态变化、右键菜单能退出。

**Step: Commit** `feat: add tray icon with status-based color and minimal context menu`

### 9c:WebView2 面板

**Files:**
- Create: `internal/trayui/panel.html`(内嵌的 HTML/CSS/JS,单文件,不拆多文件——面板功能简单,没必要上前端构建工具链)
- Create: `internal/trayui/panel.go`(webview 生命周期管理 + JS bridge 绑定)

**面板内容(对应设计文档"两种面板状态"):**
- 未 `Configured`:一个 token 输入框 + "安装并加入网络"按钮,点击调用 JS bridge 绑定的 `setupNetclient(token)`(内部调用 Task 8 的 `netclientinstall.Run`,这一步本身需要管理员权限,复用 `elevate` 包提权),面板显示进度文字(bridge 函数可以是同步阻塞返回最终结果,因为整个流程本身要几十秒,面板 JS 侧显示一个"正在安装…"的 loading 态即可,不需要做成实时进度推送这种复杂度)
- 已 `Configured`:状态色块(绿/红,对应 `Status.Healthy`)、`ServerName`、`LastBackup`;四个按钮分别绑定 `backupNow()`(跑一次 Phase 1 `backup.Run`)、`repairNow()`(跑一次 `watch.Run`)、`testConnectivity()`(手动 ping,弹一个简单输入框问对方 IP 或者默认 ping `netclient list` 里第一个 peer——具体交互细节实现时按体验调整,不是这份计划要锁死的东西)、`generateDiag()`(跑一次 `diag`,成功后用系统默认方式打开生成的 zip 所在文件夹,方便直接找到文件);另加"重新配置"按钮(同未配置状态下的 token 输入流程)
- 面板打开期间每 3 秒调用一次 `getStatus()` 刷新数据(对应设计文档)

**JS bridge**:`go-webview2` 提供 `webview.Bind(name string, fn interface{})`,把 Go 函数直接暴露给 JS 调用,不需要手写序列化协议。

**验证**:这一步最需要真机反复调,没法一次到位——交叉编译、scp、跑起来看面板长什么样、点按钮看效果,是一个"改一点、传一次、看一眼"的迭代过程,不是一次性写完就对的。第一版做到"能用、四个按钮都真实调用了对应逻辑、状态显示正确"即可,视觉细节可以后续再打磨(参考 CLAUDE.md 里"没要求的灵活性不加"的精神,不要在第一版就死磕像素级好看)。

**Step: Commit** `feat: add WebView2 panel with status dashboard and action buttons`

---

## 整体真机验证清单(Task 1-8 全部完成、Task 9 视调研结果决定是否本次一起做)

在 `100.67.147.113` / `192.168.1.172` 这台真机上(局域网 IP 优先,避免又发生 Phase 1 那种"改动本身把 SSH 依赖的隧道搞断"的情况):

1. 全新 `install`(先手动 `schtasks`/reg 清理掉 Phase 1 时期手搭的那套临时任务,避免任务名冲突或双重保护互相干扰)→ 核对 4 个计划任务、Defender 排除项、`HKCU` 启动项、`install.log` 都符合预期
2. **复现真实 bug**(和 Phase 1 一样的场景)→ 确认这次是通过正式注册的计划任务自动触发 `watch`,而不是手动跑 exe
3. `diag` → 解压核对 7 类文件都在、没有敏感字段
4. `uninstall`(不带 `--purge`)→ 核对任务/排除项/启动项清干净,`backup\` 目录还在;再 `uninstall --purge` 一次确认整个目录被清空
5. `setup-netclient` —— 需要你提供测试用 token 才能跑
6. 如果 Task 9 这次一起做:核对托盘图标颜色变化、面板四个按钮、"重新配置"流程

