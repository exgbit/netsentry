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
