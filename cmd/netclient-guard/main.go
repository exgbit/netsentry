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

	"netclient-guard/internal/autostart"
	"netclient-guard/internal/backup"
	"netclient-guard/internal/defenderexcl"
	"netclient-guard/internal/diag"
	"netclient-guard/internal/elevate"
	"netclient-guard/internal/guardlog"
	"netclient-guard/internal/schedtask"
	"netclient-guard/internal/watch"
	"netclient-guard/internal/winsvc"
)

const (
	netclientDir = `C:\Program Files (x86)\Netclient\`
	guardDir     = `C:\ProgramData\netclient-guard\`
)

func backupDir() string        { return guardDir + `backup\` }
func installLogPath() string   { return guardDir + "install.log" }
func installedExePath() string { return guardDir + "netclient-guard.exe" }

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: netclient-guard <backup|watch|diag|install|uninstall>")
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
		fmt.Println("elevate relaunch error:", err)
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

// runInstall 依次执行:自提权检查 → 复制自身到安装目录 → 注册计划任务 →
// 添加 Defender 排除 → (如果 netclient 已装好)建立备份基线 → 注册开机自启。
// 除了"复制自身"和"解析可执行文件路径"失败会直接中止,其余每一步失败都只记一条
// WARN 日志、继续跑完剩下的步骤——尽量把能装的都装上。
func runInstall() {
	ensureElevated()

	log := func(level, message string) {
		if err := guardlog.Append(installLogPath(), level, message); err != nil {
			fmt.Println("install.log write error:", err)
		}
		fmt.Printf("[%s] %s\n", level, message)
	}

	exePath, err := copySelfToInstallDir()
	if err != nil {
		log("WARN", fmt.Sprintf("copy executable to %s failed: %v", installedExePath(), err))
		exePath, err = os.Executable()
		if err != nil {
			fmt.Println("install error: cannot resolve current executable path:", err)
			os.Exit(1)
		}
	} else {
		log("INFO", "copied executable to "+exePath)
	}

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
// exe 覆盖自身触发共享冲突)。返回复制后(或本来就在)的可执行文件路径。
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
