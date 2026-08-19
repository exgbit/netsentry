// netclient-guard 是一个 Windows 后台工具,用于自动备份/恢复 netclient 的身份配置,
// 修复因 netclient.json 与 servers.json 不一致导致的启动崩溃(已知的 netclient 自愈缺陷)。
//
// Phase 1 只实现 backup/watch/diag 三个无 UI 子命令;计划任务注册、Defender 排除、
// 托盘 UI、netclient 安装/加入网络留给 Phase 2。
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"netclient-guard/internal/autostart"
	"netclient-guard/internal/backup"
	"netclient-guard/internal/defenderexcl"
	"netclient-guard/internal/diag"
	"netclient-guard/internal/elevate"
	"netclient-guard/internal/guardlog"
	"netclient-guard/internal/netclientinstall"
	"netclient-guard/internal/schedtask"
	"netclient-guard/internal/sysreport"
	"netclient-guard/internal/watch"
	"netclient-guard/internal/winsvc"
)

const (
	netclientDir = `C:\Program Files (x86)\Netclient\`
	guardDir     = `C:\ProgramData\netclient-guard\`
	guardVersion = "0.1.0-phase2"
)

func backupDir() string        { return guardDir + `backup\` }
func installLogPath() string   { return guardDir + "install.log" }
func installedExePath() string { return guardDir + "netclient-guard.exe" }

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: netclient-guard <backup|watch|diag|install|uninstall|setup-netclient>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "backup":
		runBackup()
	case "watch":
		runWatch()
	case "diag":
		runDiag()
	case "install":
		runInstall()
	case "uninstall":
		runUninstall()
	case "setup-netclient":
		runSetupNetclient()
	default:
		fmt.Println("unknown command:", os.Args[1])
		os.Exit(1)
	}
}

// ensureElevated 检查当前进程是否以管理员权限运行;不是的话发起 UAC 提权重启并退出
// 当前(非提权的)进程——提权后的子进程会带着同样的命令行参数接管剩下的工作。
func ensureElevated() {
	elevated, err := elevate.IsElevated()
	if err != nil {
		fmt.Println("elevate check error:", err)
		os.Exit(1)
	}
	if elevated {
		return
	}
	if err := elevate.RelaunchElevated(os.Args[1:]); err != nil {
		// 这个 error 既可能是"UAC 提权本身没能发起"(比如用户点了拒绝),
		// 也可能是"提权进程本身跑到一半失败退出"——见 elevate.RelaunchElevated
		// 的文档注释,两种情况区分不开也不强求,给出的错误信息已经足够明确
		// 表明整个操作没有成功。
		fmt.Println("elevated run failed (UAC declined, or the elevated process itself failed):", err)
		os.Exit(1)
	}
	os.Exit(0)
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
	svc := winsvc.SCController{Name: "netclient", LogPath: netclientDir + `logs\winsw.out.log`}
	result, err := watch.Run(netclientDir, backupDir(), svc)
	if err != nil {
		fmt.Println("watch ALERT:", err)
		os.Exit(1)
	}
	fmt.Println("watch:", result.Action, "-", result.Detail)
}

// runDiag 收集设计文档要求的 7 类诊断信息,打包成一个带时间戳的 zip:
// 1) 脱敏后的 netclient.json/servers.json(Phase 1 已有)
// 2) winsw.out.log / winsw.err.log
// 3) guard.log
// 4) 服务状态(sc.exe query + winsw.xml)
// 5) 计划任务状态
// 6) Defender 排除列表 + 相关威胁检测记录
// 7) 系统信息汇总(Windows/netclient/guard 版本、主机名、生成时间)
//
// 除了 netclient.json/servers.json 这两个核心配置文件读取失败仍然中止整个命令
// (没有它们诊断包就没有意义),其余来源都是"能采多少算多少":日志文件缺失就跳过,
// sysreport 采集失败就把错误信息本身写进对应的 txt,不让单个来源的失败拖累其余
// 已经采集到的有用信息。
func runDiag() {
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

	sources := []diag.Source{
		{Name: "config-summary/netclient.json", Data: cleanNC},
		{Name: "config-summary/servers.json", Data: cleanSrv},
	}

	for name, path := range map[string]string{
		"winsw.out.log": netclientDir + `logs\winsw.out.log`,
		"winsw.err.log": netclientDir + `logs\winsw.err.log`,
		"guard.log":     guardDir + "guard.log",
	} {
		if data, err := os.ReadFile(path); err == nil {
			sources = append(sources, diag.Source{Name: name, Data: data})
		}
	}

	sources = append(sources,
		collectSysreportSource("service-status.txt", "service status", sysreport.ServiceStatus),
		collectSysreportSource("scheduled-tasks.txt", "scheduled tasks status", sysreport.ScheduledTasksStatus),
		collectSysreportSource("defender-status.txt", "Defender status", sysreport.DefenderStatus),
		collectSysreportSource("system-info.txt", "system info", func() (string, error) { return sysreport.SystemInfo(guardVersion) }),
	)

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("diag error resolving home directory:", err)
		os.Exit(1)
	}
	outPath := home + `\Desktop\netclient-diag-` + time.Now().Format("20060102-150405") + `.zip`
	if err := diag.Bundle(sources, outPath); err != nil {
		fmt.Println("diag error writing bundle:", err)
		os.Exit(1)
	}
	fmt.Println("diag bundle written to", outPath)
}

// collectSysreportSource 跑一个 sysreport 采集函数,失败时不让整个 diag 命令失败——
// 把错误信息本身写进同一个文件名的内容里,这样诊断包里 7 类文件名永远齐全,只是
// 采集失败的那部分内容是错误说明而不是真实数据,方便使用者一眼看出"这块没采到"
// 而不是「这个文件为什么不见了」。
func collectSysreportSource(name, label string, collect func() (string, error)) diag.Source {
	content, err := collect()
	if err != nil {
		return diag.Source{Name: name, Data: []byte("error collecting " + label + ": " + err.Error())}
	}
	return diag.Source{Name: name, Data: []byte(content)}
}

// runInstall 依次执行:自提权检查 → doInstall 的实际安装步骤。
func runInstall() {
	ensureElevated()
	doInstall()
}

// doInstall 依次执行:复制自身到安装目录 → 注册计划任务 → 添加 Defender 排除 →
// (如果 netclient 已装好)建立备份基线 → 注册开机自启。
// "复制自身"失败会直接中止安装(见 copySelfToInstallDir 的文档注释——不能拿一个
// 不持久的路径去注册计划任务/开机自启);除此之外,其余每一步失败都只记一条 WARN
// 日志、继续跑完剩下的步骤——尽量把能装的都装上。
//
// 不含自提权检查:调用方要么自己是已经提权过的 runInstall,要么是
// setup-netclient(已经在下载/安装/加入网络之前做过一次 ensureElevated,不需要
// 再触发一次 UAC)。
func doInstall() {
	log := func(level, message string) {
		if err := guardlog.Append(installLogPath(), level, message); err != nil {
			fmt.Println("install.log write error:", err)
		}
		fmt.Printf("[%s] %s\n", level, message)
	}

	exePath, err := copySelfToInstallDir()
	if err != nil {
		log("ERROR", fmt.Sprintf("copy executable to %s failed: %v", installedExePath(), err))
		fmt.Println("install error: cannot copy executable to install location, aborting")
		os.Exit(1)
	}
	log("INFO", "copied executable to "+exePath)

	warnings := 0

	if err := schedtask.Register(exePath); err != nil {
		log("WARN", "register scheduled tasks failed: "+err.Error())
		warnings++
	} else {
		log("INFO", "registered scheduled tasks")
	}

	if err := defenderexcl.Add(netclientDir); err != nil {
		log("WARN", "add Defender exclusion failed: "+err.Error())
		warnings++
	} else {
		log("INFO", "added Defender exclusion for "+netclientDir)
	}

	if _, statErr := os.Stat(netclientDir + "netclient.json"); statErr == nil {
		outcome, err := backup.Run(netclientDir, backupDir())
		if err != nil {
			log("WARN", "baseline backup failed: "+err.Error())
			warnings++
		} else {
			log("INFO", "baseline backup: "+outcome.String())
		}
	} else {
		log("INFO", "netclient not installed yet, skipped baseline backup")
	}

	if err := autostart.Register(exePath); err != nil {
		log("WARN", "register autostart failed: "+err.Error())
		warnings++
	} else {
		log("INFO", "registered autostart")
	}

	if warnings == 0 {
		fmt.Println("install: completed successfully, no warnings")
	} else {
		fmt.Printf("install: completed with %d warning(s), see %s\n", warnings, installLogPath())
	}
}

// copySelfToInstallDir 把当前运行的可执行文件复制到 guardDir 下的
// netclient-guard.exe(已经是从这个路径运行时跳过复制,避免用同一个正在运行的
// exe 覆盖自身触发共享冲突;这种情况视为成功,不是错误)。返回复制后(或本来
// 就在)的可执行文件路径。
//
// 调用方(runInstall)在这里返回非 nil error 时必须直接中止安装,不能退回去
// 用 os.Executable() 的原始路径继续跑:当前运行的 exe 完全可能是从下载目录、
// 临时解压目录或者 U 盘之类的非持久位置启动的——如果拿这种路径去注册计划任务/
// 开机自启,装完看起来是成功的,但一重启或者那个临时位置一消失(U 盘拔了、
// temp 目录被清理)整套保护机制就悄无声息地失效了,比直接告诉用户装失败更糟。
func copySelfToInstallDir() (string, error) {
	src, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable path: %w", err)
	}

	dst := installedExePath()
	if samePath(src, dst) {
		return dst, nil
	}

	if err := os.MkdirAll(guardDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", guardDir, err)
	}

	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", fmt.Errorf("copy to %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close %s: %w", dst, err)
	}
	return dst, nil
}

// samePath 判断两个 Windows 路径是不是指向同一个文件(大小写不敏感)。
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// hasPurgeFlag 判断参数列表里是否包含 --purge。
func hasPurgeFlag(args []string) bool {
	for _, a := range args {
		if a == "--purge" {
			return true
		}
	}
	return false
}

// runUninstall 依次执行:自提权检查 → 注销计划任务 → 移除 Defender 排除 →
// 注销开机自启。默认保留 backupDir() 下的历史备份;带 --purge 时最后连
// guardDir 整个目录一起删——此时 guardDir 已经不存在,不再能写日志,
// 所以"删除完成"这一步只打印到 stdout,不写日志文件。
func runUninstall() {
	ensureElevated()

	purge := hasPurgeFlag(os.Args[2:])

	log := func(level, message string) {
		if err := guardlog.Append(installLogPath(), level, message); err != nil {
			fmt.Println("install.log write error:", err)
		}
		fmt.Printf("[%s] %s\n", level, message)
	}

	if err := schedtask.Unregister(); err != nil {
		log("WARN", "unregister scheduled tasks failed: "+err.Error())
	} else {
		log("INFO", "unregistered scheduled tasks")
	}

	if err := defenderexcl.Remove(netclientDir); err != nil {
		log("WARN", "remove Defender exclusion failed: "+err.Error())
	} else {
		log("INFO", "removed Defender exclusion for "+netclientDir)
	}

	if err := autostart.Unregister(); err != nil {
		log("WARN", "unregister autostart failed: "+err.Error())
	} else {
		log("INFO", "unregistered autostart")
	}

	if !purge {
		log("INFO", "uninstall complete, backups kept under "+backupDir())
		fmt.Println("uninstall: done, backups kept under", backupDir())
		return
	}

	log("INFO", "uninstall complete, purging "+guardDir)
	if err := os.RemoveAll(guardDir); err != nil {
		fmt.Println("uninstall: purge of", guardDir, "failed:", err)
		os.Exit(1)
	}
	fmt.Println("uninstall: done, purged", guardDir)
}

// tokenFlag 在参数列表里查找 -t 后面跟的值(enrollment token)。找不到 -t 或者
// -t 后面没有值,返回空字符串和 false。
func tokenFlag(args []string) (string, bool) {
	for i, a := range args {
		if a == "-t" && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// runSetupNetclient 依次执行:自提权检查 → 下载/安装/加入网络(netclientinstall.Run)
// → 联动装 guard(doInstall,复用 Task 6 的安装逻辑,不重复触发 UAC)→ 建立备份基线。
func runSetupNetclient() {
	ensureElevated()

	token, ok := tokenFlag(os.Args[2:])
	if !ok {
		fmt.Println("usage: netclient-guard setup-netclient -t <token>")
		os.Exit(1)
	}

	if err := netclientinstall.Run(token); err != nil {
		fmt.Println("setup-netclient error:", err)
		os.Exit(1)
	}

	doInstall()

	outcome, err := backup.Run(netclientDir, backupDir())
	if err != nil {
		fmt.Println("setup-netclient: baseline backup failed:", err)
		os.Exit(1)
	}
	fmt.Println("setup-netclient: completed,", outcome)
}
