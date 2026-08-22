//go:build darwin

// netsentry-mac 是 NetSentry 的 macOS 入口(Phase 1:无 GUI,只有 CLI +
// LaunchDaemon 巡检)。与 Windows 版(cmd/netsentry)共享全部核心逻辑
// (backup/watch 决策/guardconfig/selfupdate/netclientinstall),这里只实现
// macOS 的平台层:launchd 服务控制、root 权限检查、路径约定、守护 daemon 的
// 注册。刻意做成独立入口而不是在 Windows main.go 里堆 build tag——那份 main.go
// 有大量真机踩坑注释和 Windows 专属流程(UAC/托盘/计划任务),混在一起两边都
// 难维护。
//
// macOS 上的失效场景与对策(相对 Windows 的差异):
//   - netclient.json/servers.json 不一致 → 与 Windows 同源缺陷,同一套备份/恢复;
//     且 netclient 的 LaunchDaemon 是 KeepAlive 的,配置坏了会无限崩溃重启,
//     恢复前必须先 bootout(见 launchdsvc.Stop)。
//   - 睡眠唤醒后隧道卡死 → broker 卡死 + ping 门控重启,逻辑与 Windows 相同。
//   - 用户在"系统设置→登录项"里关掉 netclient 后台项 → watch 巡检发现 plist
//     没挂载会重新 bootstrap(launchdsvc.Start 兜底)。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"netsentry/internal/appversion"
	"netsentry/internal/backup"
	"netsentry/internal/guardlog"
	"netsentry/internal/launchdsvc"
	"netsentry/internal/selfupdate"
	"netsentry/internal/settings"
	"netsentry/internal/watch"
)

const (
	// netclientDir 是 netclient 的配置目录(root 权限,实机确认)。
	netclientDir = "/etc/netclient"
	// guardDir 是 NetSentry 自己的状态目录(备份、日志、settings.json)。
	guardDir = "/Library/Application Support/NetSentry"
	// installedExePath 是 netsentry 二进制的安装位置,自动升级在这个目录里做
	// 文件换入(清单 key "netsentry" 对应这里的文件名)。
	installedExePath = "/usr/local/bin/netsentry"

	// netclient 的 LaunchDaemon 信息(实机确认:`netclient install` 生成)。
	netclientLabel    = "com.gravitl.netclient"
	netclientPlist    = "/Library/LaunchDaemons/com.gravitl.netclient.plist"
	netclientLogPath  = "/var/log/com.gravitl.netclient.log"
	defaultJoinPort   = "51821"
	guardLabel        = "cn.tomtoc.netsentry.watch"
	guardPlistPath    = "/Library/LaunchDaemons/" + guardLabel + ".plist"
	watchIntervalSecs = 300
)

func backupDir() string     { return guardDir + "/backup" }
func guardLogPath() string  { return guardDir + "/guard.log" }
func settingsPath() string  { return guardDir + "/settings.json" }
func daemonLogPath() string { return guardDir + "/daemon.log" }

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: netsentry <backup|watch|install|uninstall|setup-netclient|version>")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "backup":
		ensureRoot()
		runBackup()
	case "watch":
		ensureRoot()
		runWatch()
	case "install":
		ensureRoot()
		runInstall()
	case "uninstall":
		ensureRoot()
		runUninstall()
	case "setup-netclient":
		ensureRoot()
		runSetupNetclient()
	case "version":
		fmt.Println(appversion.Guard)
	default:
		fmt.Println("unknown command:", os.Args[1])
		os.Exit(1)
	}
}

// ensureRoot 要求以 root 运行(/etc/netclient 与 launchctl system domain 都需要)。
// 不像 Windows 版那样自动发起提权——mac 终端里补一个 sudo 是常识操作,自动
// 调 sudo 反而会在无 TTY 的场景(daemon 里)卡住等密码。
func ensureRoot() {
	if os.Geteuid() != 0 {
		fmt.Println("需要 root 权限,请用 sudo 重新运行:sudo " + strings.Join(os.Args, " "))
		os.Exit(1)
	}
}

func netclientSvc() launchdsvc.Controller {
	return launchdsvc.Controller{Label: netclientLabel, PlistPath: netclientPlist, LogPath: netclientLogPath}
}

// pingTunnelChecker 与 Windows 版语义一致,ping 参数换成 macOS 的 -c。
type pingTunnelChecker struct{}

func (pingTunnelChecker) TunnelReachable() bool {
	s, _ := settings.Load(settingsPath())
	for _, ip := range s.ConnectivityTargets {
		if exec.Command("ping", "-c", "2", "-t", "3", ip).Run() == nil {
			return true
		}
	}
	return false
}

func runBackup() {
	outcome, err := backup.Run(netclientDir, backupDir())
	if err != nil {
		_ = guardlog.Append(guardLogPath(), "ERROR", "backup: "+err.Error())
		fmt.Println("backup error:", err)
		os.Exit(1)
	}
	_ = guardlog.Append(guardLogPath(), "INFO", "backup: "+outcome.String())
	fmt.Println("backup:", outcome)
}

func runWatch() {
	result, err := watch.Run(netclientDir, backupDir(), netclientSvc(), pingTunnelChecker{})
	if err != nil {
		_ = guardlog.Append(guardLogPath(), "ALERT", "watch: "+err.Error())
		fmt.Println("watch ALERT:", err)
		os.Exit(1)
	}
	_ = guardlog.Append(guardLogPath(), "INFO", fmt.Sprintf("watch: %s - %s", result.Action, result.Detail))
	fmt.Println("watch:", result.Action, "-", result.Detail)

	// 自动升级:与 Windows 版同一套验签/防降级逻辑,mac 清单是 version-mac.json,
	// 换入目录是 /usr/local/bin,节流戳等状态放 guardDir。
	s, _ := settings.Load(settingsPath())
	if upResult, upErr := selfupdate.Run(s.UpdateBaseURL, appversion.Guard, "/usr/local/bin", guardDir); upErr != nil {
		_ = guardlog.Append(guardLogPath(), "WARN", "selfupdate: "+upErr.Error())
		fmt.Println("selfupdate WARN:", upErr)
	} else if upResult.Updated {
		_ = guardlog.Append(guardLogPath(), "INFO", "selfupdate: "+upResult.Detail)
		fmt.Println("selfupdate:", upResult.Detail)
	}
}
