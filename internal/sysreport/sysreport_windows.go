//go:build windows

// Package sysreport 是给 diag 命令用的薄封装:跑个命令/读个文件、原样把文本返回,
// 不做任何解析或校验,和 winsvc 的 sc.exe 封装是同一个性质。
package sysreport

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"netsentry/internal/schedtask"
	"netsentry/internal/winexec"
)

const netclientDir = `C:\Program Files (x86)\Netclient\`

// ServiceStatus 返回 sc.exe query netclient 的输出,加上 winsw.xml 的内容
// (netclient 自己的 Windows 服务是靠 WinSW 包起来跑的,winsw.xml 是它的服务配置)。
func ServiceStatus() (string, error) {
	out, err := winexec.Hidden("sc.exe", "query", "netclient").CombinedOutput()
	scSection := string(out)
	if err != nil {
		scSection = fmt.Sprintf("sc.exe query netclient failed: %v\n%s", err, out)
	}

	var winswSection string
	if data, err := os.ReadFile(netclientDir + "winsw.xml"); err == nil {
		winswSection = string(data)
	} else {
		winswSection = fmt.Sprintf("(could not read winsw.xml: %v)", err)
	}

	return fmt.Sprintf("=== sc.exe query netclient ===\n%s\n\n=== winsw.xml ===\n%s", scSection, winswSection), nil
}

// ScheduledTasksStatus 对本工具注册的每个计划任务名跑一次
// schtasks /Query /TN <name> /FO LIST /V,拼接成一份报告。单个任务查询失败
// (比如任务还没注册)只记在对应小节里,不中断其余任务的查询。
func ScheduledTasksStatus() (string, error) {
	var report string
	for _, name := range schedtask.AllTaskNames() {
		out, err := winexec.Hidden("schtasks.exe", "/Query", "/TN", name, "/FO", "LIST", "/V").CombinedOutput()
		section := string(out)
		if err != nil {
			section = fmt.Sprintf("schtasks /Query /TN %s failed: %v\n%s", name, err, out)
		}
		report += fmt.Sprintf("=== %s ===\n%s\n\n", name, section)
	}
	return report, nil
}

// DefenderStatus 返回 Windows Defender 的排除路径列表,加上和 Netclient 路径相关的
// 威胁检测历史(用来排查"是不是 Defender 误报删了 netclient 文件")。
func DefenderStatus() (string, error) {
	exclOut, exclErr := winexec.Hidden("powershell.exe", "-NoProfile", "-Command",
		"Get-MpPreference | Select-Object -ExpandProperty ExclusionPath").CombinedOutput()
	exclSection := string(exclOut)
	if exclErr != nil {
		exclSection = fmt.Sprintf("Get-MpPreference failed: %v\n%s", exclErr, exclOut)
	}

	threatOut, threatErr := winexec.Hidden("powershell.exe", "-NoProfile", "-Command",
		"Get-MpThreatDetection | Where-Object { $_.Resources -like '*Netclient*' } | Format-List").CombinedOutput()
	threatSection := string(threatOut)
	if threatErr != nil {
		threatSection = fmt.Sprintf("Get-MpThreatDetection failed: %v\n%s", threatErr, threatOut)
	}

	return fmt.Sprintf("=== Defender exclusion paths ===\n%s\n\n=== Threat detections matching \"Netclient\" ===\n%s",
		exclSection, threatSection), nil
}

// SystemInfo 返回一份汇总的环境信息:Windows 版本、netclient 版本、guard 版本、
// 主机名和生成时间。
func SystemInfo(guardVersion string) (string, error) {
	osOut, osErr := winexec.Hidden("powershell.exe", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_OperatingSystem).Caption.Trim() + ' ' + (Get-CimInstance Win32_OperatingSystem).Version").CombinedOutput()
	osVersion := string(osOut)
	if osErr != nil {
		osVersion = fmt.Sprintf("(could not determine Windows version: %v: %s)", osErr, osOut)
	}

	netclientVersion := "(unknown)"
	if data, err := os.ReadFile(netclientDir + "netclient.json"); err == nil {
		var v struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &v); err == nil {
			netclientVersion = v.Version
		} else {
			netclientVersion = fmt.Sprintf("(could not parse netclient.json: %v)", err)
		}
	} else {
		netclientVersion = fmt.Sprintf("(could not read netclient.json: %v)", err)
	}

	hostname, hostErr := os.Hostname()
	if hostErr != nil {
		hostname = fmt.Sprintf("(could not determine hostname: %v)", hostErr)
	}

	return fmt.Sprintf(
		"Windows version: %s\nnetclient version: %s\nNetSentry version: %s\nhostname: %s\ngenerated at: %s\n",
		osVersion, netclientVersion, guardVersion, hostname, time.Now().Format(time.RFC3339)), nil
}
