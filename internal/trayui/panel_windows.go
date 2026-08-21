//go:build windows

package trayui

import (
	_ "embed"
	"encoding/json"
	"runtime"

	webview2 "github.com/jchv/go-webview2"

	"netsentry/internal/settings"
)

//go:embed panel.html
var panelHTML string

// appIconResourceID 是窗口标题栏/任务栏图标在 exe 里的 Windows 资源 ID。
//
// go-webview2 的 IconId 是通过 LoadImageW(hInstance, id, IMAGE_ICON, ...) 从 exe
// 自身编译进去的资源里按数字 ID 加载图标(不支持运行时传原始 .ico 字节),所以
// 图标本身要靠外部工具编译成 Windows 资源、链接进 exe——本仓库用
// `go run github.com/akavel/rsrc@latest -ico internal/trayui/assets/app.ico
// -arch amd64 -o cmd/netsentry/rsrc_windows_amd64.syso` 生成
// cmd/netsentry/rsrc_windows_amd64.syso(已提交,`go build` 会自动识别并链接,
// 换了图标才需要重新跑这条命令)。
//
// 这个 ID 不是随便选的、也不是靠读 rsrc 源码猜的——第一次接进来时凭读源码猜了
// "先每一帧、最后整组"、算出该是 5,结果部署到真机后标题栏图标根本没变
// (LoadImageW 找不到对应 ID,静默 fallback 成系统默认图标,不会报错提醒)。
// 后来用 debug/pe 直接解析编译出的 exe 的 .rsrc 段实测才发现顺序反了:rsrc 的
// addIcon() 其实是"先保留整组的 gid,再给每一帧发 id",代表整个多分辨率图标的
// RT_GROUP_ICON(LoadImageW 实际读取的就是这个)拿到的是 ID 1,4 个帧
// (16/32/48/256)拿到 ID 2-5。以后换图标、帧数变了,不要再凭读源码推算——用
// `go run debug/pe 小脚本` 或等价方式实测编译出的 exe 里 RT_GROUP_ICON 实际
// 拿到的 ID,和这里保持一致。
const appIconResourceID = 1

// panelWidth/panelHeight 是面板收起状态(没有操作结果展示)的固定尺寸;
// outputPanelWidth 是"执行结果"在右侧展开时额外加宽的量,由 setOutputPanelOpen
// 绑定驱动(见 bindPanel)。这三个数字和 panel.html 里的
// `.main-col { width: 380px; }` / 面板内容在健康稳态下的实际高度保持一致,
// 改一处要记得改另一处。
const (
	panelWidth       = 380
	panelHeight      = 600
	outputPanelWidth = 300
)

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

	// 面板不允许滚动(panel.html 的 .body-scroll 现在是 overflow:hidden,不再用
	// "能滚动"当内容裁切的安全网——真机反馈过光是能感觉到滚一下,体验就不对,
	// 不管滚动条显不显示)。panelHeight 比内容在健康稳态下的实际高度多留了一截
	// 余量,不是刚好贴着算出来的。
	title := "NetSentry 面板"
	if cfg.Version != "" {
		title += " v" + cfg.Version
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		WindowOptions: webview2.WindowOptions{
			Title:  title,
			Width:  panelWidth,
			Height: panelHeight,
			Center: true,
			// appIconResourceID 见下方常量注释——不设的话 go-webview2 会用
			// GetSystemMetrics 拿到的系统默认图标,标题栏/任务栏就是一个没有
			// 品牌辨识度的通用窗口图标。
			IconId: appIconResourceID,
		},
	})
	if w == nil {
		return
	}
	defer w.Destroy()
	w.SetSize(panelWidth, panelHeight, webview2.HintFixed)

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
// 已安装的 netsentry.exe 重新走一遍 CLI 子命令,见 panel.go 里 runExeCommand
// 的文档注释。
func bindPanel(w webview2.WebView, cfg PanelConfig) {
	w.Bind("getStatus", func() (Status, error) {
		status, err := Collect(cfg.NetclientDir, cfg.BackupDir, cfg.Svc)
		status.Version = cfg.Version
		return status, err
	})
	w.Bind("getChangelog", func() []ChangelogEntry {
		return Changelog
	})
	// getSettings/saveSettings 每次都直接读/写 cfg.SettingsPath,不缓存——设置
	// 界面保存之后,下一次"测试连通性"要立刻用上新值,不等托盘重启。
	w.Bind("getSettings", func() (settings.Settings, error) {
		return settings.Load(cfg.SettingsPath)
	})
	w.Bind("saveSettings", func(targets []string) {
		runAsync(w, "saveSettings", func() ActionResult {
			return saveSettings(cfg.SettingsPath, targets)
		})
	})
	// setOutputPanelOpen 把操作结果("执行结果"那一栏)在窗口右侧展开/收起——
	// 之前结果是嵌在仪表盘下面的一块深色框,真机反馈说看起来像蹦出个终端窗口。
	// 直接调 w.SetSize 而不是走 runAsync:这是个本地同步的 Win32 调用
	// (SetWindowPos),不会阻塞,而且本来就已经在消息循环线程上(和 getStatus/
	// getChangelog 一样),没必要额外开 goroutine。
	w.Bind("setOutputPanelOpen", func(open bool) {
		width := panelWidth
		if open {
			width = panelWidth + outputPanelWidth
		}
		w.SetSize(width, panelHeight, webview2.HintFixed)
	})
	w.Bind("setupNetclient", func(token string) {
		runAsync(w, "setupNetclient", func() ActionResult {
			return setupNetclientResult(cfg.ExePath, cfg.InstallLogPath, token)
		})
	})
	w.Bind("backupNow", func() {
		runAsync(w, "backupNow", func() ActionResult {
			return runExeCommand(cfg.ExePath, "backup")
		})
	})
	w.Bind("repairNow", func() {
		runAsync(w, "repairNow", func() ActionResult {
			return runExeCommand(cfg.ExePath, "watch")
		})
	})
	w.Bind("testConnectivity", func() {
		runAsync(w, "testConnectivity", func() ActionResult {
			s, _ := settings.Load(cfg.SettingsPath)
			return testConnectivity(s.ConnectivityTargets)
		})
	})
	w.Bind("generateDiag", func() {
		runAsync(w, "generateDiag", func() ActionResult {
			return generateDiag(cfg.ExePath)
		})
	})
	// uninstallNow 直接复用 CLI 的 uninstall 子命令(不带 --purge,保留备份历史,
	// 跟 CLI 默认行为一致)——它自己会走 ensureElevated() -> elevate.RelaunchElevated,
	// 面板进程本身通常不是提权跑的,这里跟 setupNetclient 是同一个模式,真机上
	// 会弹一次 UAC 提示,已经验证过这条路径能正常工作。面板 JS 侧在真正调用
	// 这个之前会先走一个确认界面,不是点了按钮就立刻卸载。
	w.Bind("uninstallNow", func() {
		runAsync(w, "uninstallNow", func() ActionResult {
			return runExeCommand(cfg.ExePath, "uninstall")
		})
	})
}

// runAsync 在后台 goroutine 里跑 fn,完成后通过 w.Dispatch/w.Eval 把结果推给
// JS 侧的 window.__onAsyncResult(action, result)。
//
// 这里之所以不能像 getStatus 那样直接同步返回结果,是因为读了 go-webview2
// 的源码确认过:Bind() 注册的函数是在原生窗口消息循环所在的那个线程上同步
// 调用的(webview.go 的 msgcb -> callbinding -> v.Call),不是丢到另一个线程
// 异步跑的。凡是会 shell 出子进程等待其跑完的操作(ping/备份/修复重试/生成
// 诊断包/安装加入网络),子进程跑多久,整个面板窗口的消息循环就冻结多久——
// 真机上点"测试连通性"复现过整个窗口卡死、连关闭按钮都点不动。
func runAsync(w webview2.WebView, action string, fn func() ActionResult) {
	go func() {
		result := fn()
		payload, err := json.Marshal(result)
		if err != nil {
			return
		}
		actionJSON, err := json.Marshal(action)
		if err != nil {
			return
		}
		w.Dispatch(func() {
			w.Eval("window.__onAsyncResult(" + string(actionJSON) + "," + string(payload) + ")")
		})
	}()
}
