//go:build windows

// Package winexec 提供跑 Windows 控制台程序但不弹控制台窗口的公共小工具。
package winexec

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// createNoWindow 是 Win32 CREATE_NO_WINDOW 的裸常量值。golang.org/x/sys/windows
// 里也有同名常量,但只用这一个值没必要为此单独引入那个模块。
const createNoWindow = 0x08000000

// Hidden 构造一个不会分配/显示控制台窗口的子进程命令。
//
// netsentry-tray.exe 是 GUI 子系统、没有自己的控制台;从它(或它 shell 出的、
// 同样没有控制台的子进程链,比如 netsentry.exe 的子命令)发起的每一次控制台
// 程序调用(sc.exe/ping.exe/schtasks.exe/powershell.exe/reg.exe 等)默认都要新建
// 一个控制台——真机上 Windows 11 的"默认终端应用"是 Windows 终端,每个新控制台
// 都被接管成一个新窗口,getStatus 每 3 秒轮询一次 sc.exe query,几分钟内就在
// 桌面堆出了几十上百个终端窗口(用户截图发现的)。CREATE_NO_WINDOW 让系统从根
// 上不分配控制台,不会触发这条路径。
func Hidden(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

// cpOEMCP 是 Win32 的 CP_OEMCP 常量(=1,"当前系统的 OEM/控制台代码页")。
// golang.org/x/sys/windows 这个旧版本(v0.1.0)没导出这个常量,直接写字面量。
const cpOEMCP = 1

// DecodeConsoleOutput 把 ping.exe/sc.exe 这类 Windows 控制台程序输出的原始字节
// 转成正确的 UTF-8 字符串。
//
// 真机踩过的坑:这些程序往 stdout 写本地化文字时,用的是系统的 OEM 代码页
// (中文 Windows 上是 GBK/CP936),不是 UTF-8——直接把 CombinedOutput() 抓到的
// 字节当 UTF-8 塞进 Go string、再 JSON 序列化传给面板 JS,中文部分会变成一堆
// 乱码方块(真机点"测试连通性"复现过,ping 输出里的中文提示全花了)。这里用
// Win32 的 MultiByteToWideChar(CP_OEMCP) 按系统实际的 OEM 代码页解码,不是
// 写死假设 GBK——理论上其他语言版本 Windows 的 OEM 代码页不一定是 GBK,这样
// 才是对的。转换失败时退回原始字节转的字符串,好过什么都不返回。
func DecodeConsoleOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	n, err := windows.MultiByteToWideChar(cpOEMCP, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n == 0 {
		return string(b)
	}
	buf := make([]uint16, n)
	if _, err := windows.MultiByteToWideChar(cpOEMCP, 0, &b[0], int32(len(b)), &buf[0], n); err != nil {
		return string(b)
	}
	return windows.UTF16ToString(buf)
}
