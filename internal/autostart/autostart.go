// Package autostart 提供构造 reg.exe 命令行参数的纯函数,用来把托盘程序注册进
// 当前用户的登录启动项。实际执行 reg.exe 的逻辑不在本文件中。
package autostart

import "strings"

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

// isValueNotFoundOutput 判断 reg.exe delete 的失败输出是不是"值本来就不存在"。
//
// 已在真实 Windows 11(简体中文)机器上实测确认:
//
//	reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v NetclientGuardDoesNotExistTest12345 /f
//	=> 错误: 系统找不到指定的注册表项或值。
//
// 英文系统下对应文案是 "ERROR: The system was unable to find the specified registry
// key or value."。注意这个文案跟 schedtask 包里 schtasks 的 "cannot find the file
// specified" 不是同一句,不能复用。团队机器可能是英文或中文 locale,所以两种子串都
// 匹配;中文只匹配核心短语"找不到指定的注册表项或值",不含"错误:"前缀,避免因
// 标点/前缀差异漏判。匹配不上的失败仍会正常上报,不会被静默吞掉。
func isValueNotFoundOutput(output string) bool {
	return strings.Contains(output, "unable to find the specified registry key or value") ||
		strings.Contains(output, "找不到指定的注册表项或值")
}
