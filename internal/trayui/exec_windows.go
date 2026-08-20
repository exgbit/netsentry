//go:build windows

package trayui

import (
	"os/exec"

	"netsentry/internal/winexec"
)

// hiddenCommand 见 internal/winexec.Hidden 的文档注释——tray 进程 shell 出的每一个
// 控制台程序调用都要走这个,避免真机上堆出一堆 Windows 终端窗口。
func hiddenCommand(name string, args ...string) *exec.Cmd {
	return winexec.Hidden(name, args...)
}

// decodeConsoleOutput 见 internal/winexec.DecodeConsoleOutput 的文档注释——凡是
// 展示给用户看的、来自 ping.exe/sc.exe 这类外部控制台程序的原始输出都要走这个,
// 不然中文 Windows 上会是一堆乱码(真机点"测试连通性"复现过)。
func decodeConsoleOutput(b []byte) string {
	return winexec.DecodeConsoleOutput(b)
}
