//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"netsentry/internal/backup"
	"netsentry/internal/guardconfig"
	"netsentry/internal/netclientinstall"
	"netsentry/internal/settings"
)

// guardPlist 是 NetSentry 巡检 daemon 的 LaunchDaemon 定义:每 5 分钟跑一次
// `netsentry watch`,与 Windows 版计划任务 NetSentryWatch 对应。StandardOut/Err
// 指向 daemon.log 仅用于排障(watch 自己会写结构化的 guard.log);它是纯追加的,
// 每 5 分钟几行、增长很慢,Phase 1 不做轮转。
var guardPlist = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>watch</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StartInterval</key>
	<integer>%d</integer>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, guardLabel, installedExePath, watchIntervalSecs, daemonLogPath(), daemonLogPath())

// runInstall 安装 NetSentry 守护:二进制放到固定位置 → 建状态目录和默认配置 →
// 注册巡检 LaunchDaemon。可重复执行(升级/重装都走这条路)。
func runInstall() {
	if err := doInstall(); err != nil {
		fmt.Println("install error:", err)
		os.Exit(1)
	}
	fmt.Println("install: 完成,巡检 daemon 每", watchIntervalSecs/60, "分钟运行一次")
}

func doInstall() error {
	if err := os.MkdirAll(backupDir(), 0o755); err != nil {
		return err
	}
	if err := settings.WriteDefaultIfMissing(settingsPath()); err != nil {
		return err
	}

	// 把当前运行的二进制复制到固定安装位置(已经从那里运行则跳过)。
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self != installedExePath {
		data, err := os.ReadFile(self)
		if err != nil {
			return err
		}
		// 目标可能正是一个正在被别的进程运行的旧版本,先改名再写入(与
		// selfupdate 的换入手法一致)。
		_ = os.Rename(installedExePath, installedExePath+".old-install")
		if err := os.WriteFile(installedExePath, data, 0o755); err != nil {
			return err
		}
		_ = os.Remove(installedExePath + ".old-install")
	}

	if err := os.WriteFile(guardPlistPath, []byte(guardPlist), 0o644); err != nil {
		return err
	}
	// 已挂载的旧定义先卸掉再挂载,保证 plist 内容变化能生效。bootout 对
	// 没挂载的 label 报错,忽略。
	_ = exec.Command("launchctl", "bootout", "system/"+guardLabel).Run()
	if out, err := exec.Command("launchctl", "bootstrap", "system", guardPlistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runUninstall 卸载 NetSentry 守护,并联动卸载 netclient(与 Windows 版
// runUninstall 的行为对齐:setup-netclient 装上的东西,uninstall 全部清走)。
func runUninstall() {
	_ = exec.Command("launchctl", "bootout", "system/"+guardLabel).Run()
	if err := os.Remove(guardPlistPath); err != nil && !os.IsNotExist(err) {
		fmt.Println("uninstall WARN: 删除 plist 失败:", err)
	}
	if err := netclientinstall.Uninstall(); err != nil {
		fmt.Println("uninstall WARN: 卸载 netclient 失败:", err)
	} else {
		fmt.Println("uninstall: 已卸载 netclient")
	}
	if err := os.RemoveAll(guardDir); err != nil {
		fmt.Println("uninstall WARN: 删除状态目录失败:", err)
	}
	// 自己的二进制留给调用方删(rm /usr/local/bin/netsentry),进程删除自己的
	// 镜像文件在 mac 上其实是允许的,但留着也方便"卸了又想装回来"。
	fmt.Println("uninstall: 完成(如需彻底删除,执行 sudo rm", installedExePath, ")")
}

// runSetupNetclient 与 Windows 版同一套流程:已装且健康 → 跳过重装只开守护;
// 已装但异常 → 完整卸载重装;没装 → 全新安装。最后注册守护 + 建立备份基线。
func runSetupNetclient() {
	token, ok := stringFlag(os.Args[2:], "-t")
	if !ok {
		fmt.Println("usage: netsentry setup-netclient -t <token> [-p <port>] [--name <device-name>]")
		os.Exit(1)
	}
	port, ok := stringFlag(os.Args[2:], "-p")
	if !ok {
		port = defaultJoinPort
	}
	name, ok := stringFlag(os.Args[2:], "--name")
	if !ok {
		if h, err := os.Hostname(); err == nil {
			name = strings.TrimSuffix(h, ".local")
		}
	}

	load, loadErr := guardconfig.Load(netclientDir)
	_, plistErr := os.Stat(netclientPlist)
	action := netclientinstall.DecideExisting(
		netclientinstall.Installed(), loadErr == nil && load.Consistent, plistErr == nil)

	switch action {
	case netclientinstall.KeepAndGuard:
		fmt.Println("setup-netclient: 检测到本机已安装 netclient 且配置正常,跳过重装,直接开启守护")
		svc := netclientSvc()
		if running, err := svc.IsRunning(); err != nil || !running {
			if err := svc.Start(); err != nil {
				fmt.Println("setup-netclient WARN: 启动 netclient 失败(守护巡检稍后会重试):", err)
			} else {
				fmt.Println("setup-netclient: 已启动 netclient daemon")
			}
		}
	case netclientinstall.WipeAndReinstall:
		fmt.Println("setup-netclient: 检测到本机已安装 netclient 但配置异常,先完整卸载再重新安装")
		if err := netclientinstall.Uninstall(); err != nil {
			fmt.Println("setup-netclient error: 卸载异常的 netclient 失败:", err)
			os.Exit(1)
		}
		fallthrough
	case netclientinstall.FreshInstall:
		if err := netclientinstall.Run(token, port, name); err != nil {
			fmt.Println("setup-netclient error:", err)
			os.Exit(1)
		}
	}

	if err := doInstall(); err != nil {
		fmt.Println("setup-netclient error: 安装守护失败:", err)
		os.Exit(1)
	}

	outcome, err := backup.Run(netclientDir, backupDir())
	if err != nil {
		fmt.Println("setup-netclient: baseline backup failed:", err)
		os.Exit(1)
	}
	fmt.Println("setup-netclient: completed,", outcome)
}

// stringFlag 与 Windows 版 main.go 里的同名函数一致(入口独立、不便共享,逻辑
// 保持逐字相同)。
func stringFlag(args []string, name string) (string, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}
