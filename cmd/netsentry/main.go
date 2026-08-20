// NetSentry 是一个 Windows 后台工具,用于自动备份/恢复 netclient 的身份配置,
// 修复因 netclient.json 与 servers.json 不一致导致的启动崩溃(已知的 netclient 自愈缺陷)。
//
// Phase 1 只实现 backup/watch/diag 三个无 UI 子命令;计划任务注册、Defender 排除、
// 托盘 UI、netclient 安装/加入网络留给 Phase 2。
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/getlantern/systray"

	"netsentry/internal/autostart"
	"netsentry/internal/backup"
	"netsentry/internal/defenderexcl"
	"netsentry/internal/diag"
	"netsentry/internal/elevate"
	"netsentry/internal/guardlog"
	"netsentry/internal/netclientinstall"
	"netsentry/internal/netpriority"
	"netsentry/internal/schedtask"
	"netsentry/internal/selfcleanup"
	"netsentry/internal/settings"
	"netsentry/internal/sysreport"
	"netsentry/internal/trayui"
	"netsentry/internal/watch"
	"netsentry/internal/winsvc"
)

const (
	netclientDir = `C:\Program Files (x86)\Netclient\`
	guardDir     = `C:\ProgramData\NetSentry\`
	guardVersion = "0.5.3"
)

func backupDir() string        { return guardDir + `backup\` }
func installLogPath() string   { return guardDir + "install.log" }
func guardLogPath() string     { return guardDir + "guard.log" }
func settingsPath() string     { return guardDir + "settings.json" }
func installedExePath() string { return guardDir + "netsentry.exe" }

// installedTrayExePath 返回装好之后 GUI 子系统版本的可执行文件路径——见本文件顶部
// "子系统选择"的说明:tray 专用这一份是单独用 -H=windowsgui 编译的产物,和
// installedExePath() 那份 console 子系统的可执行文件是同一份源码的两个构建
// 产物,不是两个不同的程序。
func installedTrayExePath() string { return guardDir + "netsentry-tray.exe" }

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: netsentry <backup|watch|diag|install|uninstall|setup-netclient|tray>")
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
	case "tray":
		runTray()
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
		_ = guardlog.Append(guardLogPath(), "ERROR", "backup: "+err.Error())
		fmt.Println("backup error:", err)
		os.Exit(1)
	}
	_ = guardlog.Append(guardLogPath(), "INFO", "backup: "+outcome.String())
	fmt.Println("backup:", outcome)
}

func runWatch() {
	svc := winsvc.SCController{Name: "netclient", LogPath: netclientDir + `logs\winsw.out.log`}
	result, err := watch.Run(netclientDir, backupDir(), svc)
	if err != nil {
		_ = guardlog.Append(guardLogPath(), "ALERT", "watch: "+err.Error())
		fmt.Println("watch ALERT:", err)
		os.Exit(1)
	}
	_ = guardlog.Append(guardLogPath(), "INFO", fmt.Sprintf("watch: %s - %s", result.Action, result.Detail))
	fmt.Println("watch:", result.Action, "-", result.Detail)

	// netmaker 网卡跃点数过低会让 DNS/路由被 VPN 抢优先级、上网变慢(真实反馈的
	// 问题,见 internal/netpriority 包文档)。跟主 watch 逻辑是两件独立的事,
	// 失败不影响 watch 本身的退出码——这只是个体验优化,不是"配置损坏"级别的
	// 故障,不需要让计划任务运行历史显示成失败。
	if npResult, npErr := netpriority.Fix(); npErr != nil {
		_ = guardlog.Append(guardLogPath(), "WARN", "netpriority: "+npErr.Error())
		fmt.Println("netpriority WARN:", npErr)
	} else if npResult.Applied {
		_ = guardlog.Append(guardLogPath(), "INFO", "netpriority: "+npResult.Detail)
		fmt.Println("netpriority:", npResult.Detail)
	}
}

// runTray 启动托盘图标的原生事件循环,阻塞到 systray.Quit() 被调用为止。
//
// 子系统选择(真机踩坑记录,不要在不了解下文的情况下"顺手"改构建方式):
// 这份源码要编译成两个产物——console 子系统的 netsentry.exe(backup/watch/
// install/uninstall/diag/setup-netclient 走这个,人在已经打开的 PowerShell/
// cmd 里手动跑,或者被计划任务调起)和 GUI 子系统的 netsentry-tray.exe(只给
// tray 这条路径用,装完之后 HKCU 开机自启指向的就是它)。见仓库根目录
// Makefile 里两条构建命令。
//
// 起因:tray 通过开机自启(HKCU Run 项)或双击拉起时没有继承任何已有控制台——
// 如果整个程序编译成默认的 console 子系统,Windows 会在这种情况下自动新分配
// 一个控制台窗口给它,这个窗口会显示在桌面上,而且它是这个进程唯一的控制台,
// 用户一旦手滑关掉这个窗口,整个 tray 进程和图标就跟着被杀掉——这正是真机测试
// 发现的 bug。用 `-ldflags="-H=windowsgui"` 编译能让 Windows 完全不给这个
// 进程分配控制台,从根上不会弹出这个窗口。
//
// 但不能因此把*整个*二进制(包括 backup/watch/install 等子命令)都编译成
// GUI 子系统:已经调研确认——GUI 子系统的进程即使是从一个已经打开的、有
// 交互式会话的 PowerShell/cmd 里手动跑起来的,也不会附着(attach)到那个
// 已有的控制台上,fmt.Println 等标准输出没有地方可写,在终端里看不到任何
// 输出(这是 Windows 本身"GUI 子系统进程不继承调用者控制台"的行为,不是 Go
// 或这份代码的 bug,搜了 Go 官方 golang-nuts 邮件列表和多个独立来源确认过,
// 不是靠猜的)。这些子命令的输出是人工运维/日志采集依赖的东西,不能悄悄
// 丢掉,所以它们必须留在 console 子系统的 netsentry.exe 里。
//
// 两个产物是同一份源码分别用不同 `-H` 链接参数编译出来的,不是两套不同的
// 逻辑——install 时会把跟当前正在运行的 netsentry.exe 放在同一目录下的
// netsentry-tray.exe 一并复制进安装目录,开机自启注册指向复制后的
// netsentry-tray.exe(见 copyTrayExeToInstallDir/doInstall)。
func runTray() {
	systray.Run(onTrayReady, onTrayExit)
}

// onTrayReady 设置初始图标、注册右键菜单("打开面板"/"退出"/"重启托盘"),并起一个
// 30 秒 ticker 在后台循环刷新图标颜色(对应设计文档"图标状态每 30 秒刷新一次")。
//
// 计划原本要求"左键点击弹出面板",但 getlantern/systray v1.2.2 的 Windows 实现里
// 左键和右键在原生层面(wndProc 的 WM_LBUTTONUP/WM_RBUTTONUP 分支)都只会触发同一个
// showMenu(),没有单独的 SetOnClick/IMenu 之类的 API 能把两者区分开(9b 调研结论),
// 所以改成右键菜单里加一项"打开面板",不做左键单独弹面板。
func onTrayReady() {
	systray.SetTooltip("NetSentry")

	svc := winsvc.SCController{Name: "netclient", LogPath: netclientDir + `logs\winsw.out.log`}

	refreshIcon := func() {
		status, err := trayui.Collect(netclientDir, backupDir(), svc)
		if err != nil {
			// Collect 失败(比如 netclient.json 都读不到)按"不健康"处理,不让托盘卡死
			systray.SetIcon(trayui.IconFor(false))
			return
		}
		systray.SetIcon(trayui.IconFor(status.Healthy))
	}
	refreshIcon()

	openPanelItem := systray.AddMenuItem("打开面板", "打开状态面板")
	quitItem := systray.AddMenuItem("退出", "退出 NetSentry 托盘")
	restartItem := systray.AddMenuItem("重启托盘", "重新拉起一个托盘进程(面板卡死等极端情况的兜底手段)")

	// settings.json 格式坏了(比如管理员手改的时候手滑)不会阻塞托盘启动——
	// settings.Load/面板的 getSettings/saveSettings 绑定都有兜底,这里只是提前
	// 探一次、记一条 WARN 到 guard.log,让管理员能发现"改坏了"这件事,而不是
	// 悄悄回退到默认值、自己却毫无察觉。
	if _, err := settings.Load(settingsPath()); err != nil {
		_ = guardlog.Append(guardLogPath(), "WARN", "settings.json 解析失败,已回退到默认连通性测试目标: "+err.Error())
	}

	panelCfg := trayui.PanelConfig{
		ExePath:        installedExePath(),
		NetclientDir:   netclientDir,
		BackupDir:      backupDir(),
		InstallLogPath: installLogPath(),
		Version:        guardVersion,
		Svc:            svc,
		SettingsPath:   settingsPath(),
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshIcon()
		}
	}()

	go func() {
		for {
			select {
			case <-openPanelItem.ClickedCh:
				// 每次点击都新建一个面板窗口,自己的 goroutine 里跑自己的消息循环,
				// 不阻塞这里继续响应"退出"/"重启托盘"。
				go trayui.ShowPanel(panelCfg)
			case <-quitItem.ClickedCh:
				systray.Quit()
				return
			case <-restartItem.ClickedCh:
				restartTray()
				return
			}
		}
	}()
}

func onTrayExit() {}

// restartTray 拉起一个新的 tray 进程,再让当前进程退出——处理面板卡死等
// 极端情况的兜底手段。新进程启动失败也照常退出当前进程,不重试。
func restartTray() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("restart tray: resolve current executable path failed:", err)
	} else if err := exec.Command(exePath, "tray").Start(); err != nil {
		fmt.Println("restart tray: start new process failed:", err)
	}
	systray.Quit()
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

	src, err := os.Executable()
	if err != nil {
		log("ERROR", "resolve current executable path failed: "+err.Error())
		fmt.Println("install error: cannot resolve current executable path, aborting")
		os.Exit(1)
	}

	exePath, err := copySelfToInstallDir(src)
	if err != nil {
		log("ERROR", fmt.Sprintf("copy executable to %s failed: %v", installedExePath(), err))
		fmt.Println("install error: cannot copy executable to install location, aborting")
		os.Exit(1)
	}
	log("INFO", "copied executable to "+exePath)

	warnings := 0

	// netsentry-tray.exe(GUI 子系统,专供 tray 用,见 runTray 上方"子系统选择"的
	// 说明)约定跟当前正在运行的 netsentry.exe 放在同一目录下一起分发。找不到它
	// 不中止整个安装——backup/watch 等核心保护功能不依赖它,只是跳过开机自启注册,
	// 记一条 WARN 让人知道去补上这个文件再重新装一遍。
	trayExePath, err := copyTrayExeToInstallDir(src)
	if err != nil {
		log("WARN", "copy netsentry-tray.exe failed: "+err.Error()+" (tray autostart not registered; put netsentry-tray.exe next to netsentry.exe and re-run install)")
		warnings++
	} else {
		log("INFO", "copied tray executable to "+trayExePath)
	}

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

	// 只在 settings.json 不存在时写默认值,已存在就不碰——保护管理员已经手动
	// 改过的连通性测试目标 IP 不会被重装/升级冲掉。
	if err := settings.WriteDefaultIfMissing(settingsPath()); err != nil {
		log("WARN", "write default settings.json failed: "+err.Error())
		warnings++
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

	if trayExePath != "" {
		if err := autostart.Register(trayExePath); err != nil {
			log("WARN", "register autostart failed: "+err.Error())
			warnings++
		} else {
			log("INFO", "registered autostart")
		}
	}

	if warnings == 0 {
		fmt.Println("install: completed successfully, no warnings")
	} else {
		fmt.Printf("install: completed with %d warning(s), see %s\n", warnings, installLogPath())
	}
}

// copySelfToInstallDir 把当前运行的可执行文件(src)复制到 guardDir 下的
// netsentry.exe(已经是从这个路径运行时跳过复制,避免用同一个正在运行的
// exe 覆盖自身触发共享冲突;这种情况视为成功,不是错误)。返回复制后(或本来
// 就在)的可执行文件路径。
//
// 调用方(doInstall)在这里返回非 nil error 时必须直接中止安装,不能退回去
// 用 src 的原始路径继续跑:当前运行的 exe 完全可能是从下载目录、临时解压
// 目录或者 U 盘之类的非持久位置启动的——如果拿这种路径去注册计划任务/
// 开机自启,装完看起来是成功的,但一重启或者那个临时位置一消失(U 盘拔了、
// temp 目录被清理)整套保护机制就悄无声息地失效了,比直接告诉用户装失败更糟。
func copySelfToInstallDir(src string) (string, error) {
	dst := installedExePath()
	if samePath(src, dst) {
		return dst, nil
	}
	if err := os.MkdirAll(guardDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", guardDir, err)
	}
	if err := copyFile(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// copyTrayExeToInstallDir 把 GUI 子系统版本的 netsentry-tray.exe(见 runTray
// 上方"子系统选择"的说明)从 src(当前运行的 console 版 netsentry.exe)所在
// 目录复制到 guardDir。两个 exe 是同一份源码的两种构建产物,约定放在同一
// 目录下一起分发。src 所在目录下找不到 netsentry-tray.exe 时返回 error,
// 由调用方(doInstall)记一条 WARN 并跳过开机自启注册,不中止整个安装。
func copyTrayExeToInstallDir(src string) (string, error) {
	trayExeSrc := filepath.Join(filepath.Dir(src), "netsentry-tray.exe")
	dst := installedTrayExePath()
	if samePath(trayExeSrc, dst) {
		return dst, nil
	}
	if _, err := os.Stat(trayExeSrc); err != nil {
		return "", fmt.Errorf("find netsentry-tray.exe next to %s: %w", src, err)
	}
	if err := os.MkdirAll(guardDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", guardDir, err)
	}
	if err := copyFile(trayExeSrc, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// copyFile 把 src 整个文件复制到 dst,给 copySelfToInstallDir/
// copyTrayExeToInstallDir 共用。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy to %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
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

	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("uninstall: purge of", guardDir, "failed:", err)
		os.Exit(1)
	}

	if !samePath(exePath, installedExePath()) {
		// 当前运行的不是装在 guardDir 下的那个 exe(比如从下载目录跑的另一份
		// 拷贝),guardDir 里没有正在被自己占用的文件,直接整个删掉即可。
		if err := os.RemoveAll(guardDir); err != nil {
			fmt.Println("uninstall: purge of", guardDir, "failed:", err)
			os.Exit(1)
		}
		fmt.Println("uninstall: done, purged", guardDir)
		return
	}

	// 当前运行的就是 guardDir 下那个正在被执行的 netsentry.exe——Windows 不允许
	// 删除正在运行的可执行文件镜像本身(unlinkat ... Access is denied)。同理,
	// 如果这时候 netsentry-tray.exe 作为 tray 进程还在后台跑着,它也会被锁住。
	// 先删掉这两者以外的一切,再交给一个独立于当前进程的 helper 进程,在(有)
	// 相应进程退出、释放对 exe 的文件句柄之后,异步删掉这两个 exe 本身和(届时
	// 已经空了的)目录。
	entries, err := os.ReadDir(guardDir)
	if err != nil {
		fmt.Println("uninstall: purge of", guardDir, "failed:", err)
		os.Exit(1)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	skip := []string{filepath.Base(installedExePath()), filepath.Base(installedTrayExePath())}
	for _, name := range entriesToDeleteExceptSelf(names, skip) {
		if err := os.RemoveAll(guardDir + name); err != nil {
			fmt.Println("uninstall: purge of", guardDir, "failed:", err)
			os.Exit(1)
		}
	}

	if err := selfcleanup.SpawnDelayedRemoveAll(guardDir); err != nil {
		fmt.Println("uninstall: purge of", guardDir, "failed:", err)
		os.Exit(1)
	}
	fmt.Println("uninstall: done,", guardDir, "will finish being removed in the background shortly")
}

// entriesToDeleteExceptSelf 从 guardDir 下的条目名单里筛出除了 keepNames(不区分
// 大小写,即当前正在运行、可能被锁住的可执行文件们)之外都应该立即删除的条目——
// keepNames 里的条目要留给 selfcleanup 的 helper 进程在相应进程退出之后异步删除。
func entriesToDeleteExceptSelf(names []string, keepNames []string) []string {
	keep := make([]string, 0, len(names))
	for _, n := range names {
		skip := false
		for _, kn := range keepNames {
			if strings.EqualFold(n, kn) {
				skip = true
				break
			}
		}
		if !skip {
			keep = append(keep, n)
		}
	}
	return keep
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
		fmt.Println("usage: netsentry setup-netclient -t <token>")
		os.Exit(1)
	}

	if err := netclientinstall.Run(token); err != nil {
		fmt.Println("setup-netclient error:", err)
		os.Exit(1)
	}

	// 刚加入网络、netmaker 网卡刚出现的这一刻就把跃点数调好,不用等下一次
	// (最长 5 分钟后)watch 巡检——不然新加入的同事马上就会感觉到网页很慢。
	// 失败只记日志、不中止整个 setup-netclient(同 runWatch 里的处理方式)。
	if npResult, npErr := netpriority.Fix(); npErr != nil {
		_ = guardlog.Append(guardLogPath(), "WARN", "netpriority: "+npErr.Error())
		fmt.Println("netpriority WARN:", npErr)
	} else if npResult.Applied {
		_ = guardlog.Append(guardLogPath(), "INFO", "netpriority: "+npResult.Detail)
		fmt.Println("netpriority:", npResult.Detail)
	}

	doInstall()

	outcome, err := backup.Run(netclientDir, backupDir())
	if err != nil {
		fmt.Println("setup-netclient: baseline backup failed:", err)
		os.Exit(1)
	}
	fmt.Println("setup-netclient: completed,", outcome)
}
