//go:build windows

// Package startmenu 管理"开始菜单"里指向 NetSentry 面板的快捷方式。
//
// 真机反馈过:C:\ProgramData 默认是隐藏文件夹,同事装完之后自己去找
// netsentry-tray.exe 手动运行,根本不知道要先在资源管理器里打开"显示隐藏的
// 项目"才能看到这个目录。加一个开始菜单快捷方式,让 NetSentry 能像正常装好
// 的软件一样被搜索到、点开就能用,不需要教会每个人怎么找隐藏文件夹——这不是
// 移动安装目录(guardDir 保持在 ProgramData,机器级别的位置对一个装 Windows
// 服务的工具来说是对的),只是让它变得"可发现"。
package startmenu

import (
	"fmt"
	"os"
	"path/filepath"

	"netsentry/internal/winexec"
)

// shortcutPath 返回当前登录用户开始菜单里 NetSentry 快捷方式应该在的路径——
// 放在当前用户目录下,不是所有用户共享的开始菜单,跟 autostart 包用 HKCU
// (而不是 HKLM)是同一个"只影响当前登录用户"的范围选择,不需要额外的
// 每用户/所有用户的区分逻辑。
func shortcutPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("APPDATA environment variable not set")
	}
	return filepath.Join(appData, `Microsoft\Windows\Start Menu\Programs\NetSentry.lnk`), nil
}

// Register 在当前用户的开始菜单里创建(或覆盖)一个指向 trayExePath 的快捷方式。
// 用 PowerShell 的 WScript.Shell COM 对象生成 .lnk——Go 标准库没有直接创建
// Windows 快捷方式的能力,引入专门的库对这一个功能来说不值得,复用项目里已经
// 在用的"shell 到 powershell.exe"这个模式更简单。
func Register(trayExePath string) error {
	path, err := shortcutPath()
	if err != nil {
		return err
	}
	script := fmt.Sprintf(
		"$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s'); $s.TargetPath='%s'; $s.Description='NetSentry VPN 客户端自愈助手'; $s.Save()",
		path, trayExePath,
	)
	out, err := winexec.Hidden("powershell.exe", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create start menu shortcut: %w: %s", err, winexec.DecodeConsoleOutput(out))
	}
	return nil
}

// Unregister 删除开始菜单快捷方式。不存在时不算错误——uninstall 要能在部分
// 安装/重复卸载的情况下正常跑完,不半途而废。
func Unregister() error {
	path, err := shortcutPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
