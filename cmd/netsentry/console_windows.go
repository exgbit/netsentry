//go:build windows

package main

import "golang.org/x/sys/windows"

// kernel32SetConsoleOutputCP 用 LazyDLL 方式调 SetConsoleOutputCP——项目锁定的
// golang.org/x/sys v0.1.0 没有导出这个函数的封装,为一个调用升级依赖不值得。
var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

// init 把当前控制台的输出代码页切到 UTF-8(65001)。
//
// 同事在桌面终端(cmd/Windows 终端)里跑 netsentry 时,控制台默认代码页是
// GBK(936),而 Go 程序输出的是 UTF-8 字节——所有中文输出(下载进度、
// netpriority 提示等)都会显示成乱码。真机抓过输出字节确认过是 UTF-8。这里
// 在进程启动时把控制台切到 UTF-8,让中文正常显示。没有控制台时(tray 进程、
// 被 CombinedOutput 捕获的子进程)调用失败,忽略即可,无副作用。
//
// 不做"退出时恢复原代码页":netsentry 的子命令都是跑完就退出的短进程,恢复
// 时机不可靠(os.Exit 不走 defer);代码页变更只影响当前这个终端窗口,属于
// 可接受的轻微副作用,常见 CLI 工具也是这么处理的。
func init() {
	_, _, _ = procSetConsoleOutputCP.Call(65001)
}
