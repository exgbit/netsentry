//go:build !windows

package trayui

import "os/exec"

// hiddenCommand 在非 Windows 平台上没有控制台窗口的概念,直接透传给 exec.Command。
func hiddenCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// decodeConsoleOutput 在非 Windows 平台上没有 OEM 代码页的概念,原样按 UTF-8 处理。
func decodeConsoleOutput(b []byte) string {
	return string(b)
}
