//go:build windows

// Package defenderexcl 管理 Windows Defender 的排除路径列表。
package defenderexcl

import (
	"fmt"
	"os/exec"
)

// Add 把 path 加入 Windows Defender 排除列表。Add-MpPreference 本身对重复添加是
// 幂等的,已经在排除列表里时重复调用不报错。
func Add(path string) error {
	return runMpPreference("Add-MpPreference", path)
}

// Remove 从排除列表移除 path。Remove-MpPreference 本身对删除不存在的项是幂等的,
// 本来就不在列表里时不视为错误。
func Remove(path string) error {
	return runMpPreference("Remove-MpPreference", path)
}

// runMpPreference 执行 <cmdlet> -ExclusionPath '<path>'。
//
// 关于单引号拼接:PowerShell 单引号字符串是字面量字符串,反斜杠不需要转义
// (不同于双引号字符串或大多数其他语言),所以 Windows 路径(如
// `C:\Program Files (x86)\Netclient\`)本身可以直接塞进单引号里,不用处理反斜杠。
// 唯一需要考虑的是 path 里出现单引号(PowerShell 单引号字符串里靠连写两个单引号
// 转义)会提前闭合字符串——但本函数唯一的调用方(install/uninstall)传入的都是编译期
// 写死的固定安装目录,不是任意外部输入,不存在"注入"这个威胁模型,故不做额外
// 转义/校验,和 Task 2 里 ResumeTriggerTaskXML 对 exePath 不做 XML 转义是同一个
// 判断依据。
func runMpPreference(cmdlet, path string) error {
	cmd := fmt.Sprintf("%s -ExclusionPath '%s'", cmdlet, path)
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", cmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell %s: %w: %s", cmdlet, err, out)
	}
	return nil
}
