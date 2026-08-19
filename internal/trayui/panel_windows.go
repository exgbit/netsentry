//go:build windows

package trayui

import (
	_ "embed"
	"os/exec"
	"runtime"
	"strings"

	webview2 "github.com/jchv/go-webview2"
)

//go:embed panel.html
var panelHTML string

// ShowPanel 创建一个新的面板窗口,阻塞直到用户关闭它为止——调用方应该在独立的
// goroutine 里调用本函数(main.go 的托盘菜单点击就是这么做的),否则面板开着的
// 时候托盘会没法响应"退出"/"重启托盘"点击。每次点击"打开面板"都新建一个窗口、
// 关闭时销毁,不做窗口复用(YAGNI,面板功能简单没必要)。
//
// Windows 的消息队列是跟 OS 线程绑定的、不是跟 Go 的 goroutine 绑定,所以这里用
// runtime.LockOSThread 把"创建窗口 + 跑消息循环"整个过程钉在同一个 OS 线程上,
// 避免 Go 调度器把 goroutine 挪到别的线程导致后续窗口消息收不到。
func ShowPanel(cfg PanelConfig) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		WindowOptions: webview2.WindowOptions{
			Title:  "netclient-guard 面板",
			Width:  360,
			Height: 480,
			Center: true,
		},
	})
	if w == nil {
		return
	}
	defer w.Destroy()
	w.SetSize(360, 480, webview2.HintFixed)

	bindPanel(w, cfg)
	w.SetHtml(panelHTML)
	w.Run()
}

// bindPanel 把面板 JS 需要的全部 bridge 函数绑定到 webview 上。必须在 SetHtml
// 之前调用——Bind 内部通过 webview.Init 注入脚本,只有在页面加载前注入的脚本
// 才能保证页面加载完成时 window.xxx 已经存在。
//
// getStatus 是唯一直接在 tray 进程内调用 trayui.Collect 的 bridge 函数(纯本地
// 文件读取,不需要提权,没必要额外 shell 出一个子进程);其余几个都是 shell 到
// 已安装的 netclient-guard.exe 重新走一遍 CLI 子命令,见 panel.go 里 runExeCommand
// 的文档注释。
func bindPanel(w webview2.WebView, cfg PanelConfig) {
	w.Bind("getStatus", func() (Status, error) {
		return Collect(cfg.NetclientDir, cfg.BackupDir, cfg.Svc)
	})
	w.Bind("setupNetclient", func(token string) ActionResult {
		return setupNetclientResult(cfg.ExePath, cfg.InstallLogPath, token)
	})
	w.Bind("backupNow", func() ActionResult {
		return runExeCommand(cfg.ExePath, "backup")
	})
	w.Bind("repairNow", func() ActionResult {
		return runExeCommand(cfg.ExePath, "watch")
	})
	w.Bind("testConnectivity", func(ip string) ActionResult {
		out, err := exec.Command("ping.exe", "-n", "3", ip).CombinedOutput()
		return ActionResult{Success: err == nil, Output: strings.TrimSpace(string(out))}
	})
	w.Bind("generateDiag", func() ActionResult {
		return generateDiag(cfg.ExePath)
	})
}
