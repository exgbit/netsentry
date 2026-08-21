//go:build !windows

package winexec

import "os/exec"

// Hidden 在非 Windows 平台上没有"不弹控制台窗口"这个概念,直接透传给
// exec.Command——只是为了让 go build ./... 能跨平台通过,main.go 现在直接
// 引用这个包(isTrayRunning),不再只是被其他 windows-only 文件间接引用。
func Hidden(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// DecodeConsoleOutput 在非 Windows 平台上没有 OEM 代码页的概念,原样按 UTF-8 处理。
func DecodeConsoleOutput(b []byte) string {
	return string(b)
}
