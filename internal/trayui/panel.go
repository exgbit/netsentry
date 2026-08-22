package trayui

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"netsentry/internal/settings"
)

// PanelConfig 是面板需要的全部依赖,由 main.go 组装后传给 ShowPanel。
type PanelConfig struct {
	ExePath        string // 已安装的 netsentry.exe 路径,面板里所有需要跑子命令的操作都 shell 到这个路径
	NetclientDir   string
	BackupDir      string
	InstallLogPath string // guardDir + "install.log",setupNetclient 失败且没有可展示输出时,指引用户去看这个文件
	Version        string // NetSentry 自身版本号,窗口标题栏展示
	Svc            interface{ IsRunning() (bool, error) }
	// SettingsPath 是 settings.json 的路径("测试连通性"目标 IP 等设置存在这里)。
	// getSettings/saveSettings 绑定每次都重新读这个文件,不是像早期版本那样在
	// main.go 启动时读一次、缓存进 ConnectivityTargets 字段——用户在面板"设置"
	// 里改完保存后,不重启托盘就要立刻对下一次"测试连通性"生效,缓存住旧值做
	// 不到这一点。
	SettingsPath string
}

// ActionResult 是面板按钮触发的子命令操作(backupNow/repairNow/generateDiag/
// setupNetclient/testConnectivity)统一返回给 JS 的结果:Output 是子进程的合并
// 输出,面板 JS 侧直接原样展示,不做进一步解析。
type ActionResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
}

// runExeCommand 跑一次 <exePath> 加若干参数,把子进程的合并输出和退出结果打包
// 成 ActionResult 返回给 JS——backupNow/repairNow/setupNetclient 都是这个模式:
// tray 进程本身不提权、也不直接调用 backup/watch/netclientinstall 包,而是 shell
// 到已安装的 netsentry.exe 重新走一遍 CLI 子命令,复用同一套已经测试过的
// 代码路径,并且让 setup-netclient 自己的 ensureElevated() 正确处理提权边界。
func runExeCommand(exePath string, args ...string) ActionResult {
	out, err := hiddenCommand(exePath, args...).CombinedOutput()
	return ActionResult{Success: err == nil, Output: strings.TrimSpace(string(out))}
}

// setupNetclientResult 跑一次 `<exePath> setup-netclient -t <token>`。这个子命令
// 自己会走 ensureElevated() -> elevate.RelaunchElevated,真正的安装/加入网络过程
// 发生在 -Verb RunAs 拉起的另一个提权进程里——这里 CombinedOutput() 捕获到的只是
// 包装用的 powershell.exe 自己的输出(通常是空的),看不到那个真正执行安装的进程的
// stdout。退出码依然能如实透传(RelaunchElevated 的文档注释里说明过),所以
// Success 是准的,但失败时 Output 经常是空字符串,面板上看起来就是一句干巴巴的
// "失败"、没有任何原因。这里在"失败且没有任何输出"时补一句指引,指向
// doInstall/doSetupNetclient 已经在写的 install.log——好过什么都不说。
func setupNetclientResult(exePath, installLogPath, token, port, name string) ActionResult {
	// port/name 为空时不传对应 flag,由 setup-netclient 子命令自己兜底默认值
	// (端口 51821、设备名取本机 hostname),面板和 CLI 两条入口保持同一套默认逻辑。
	args := []string{"setup-netclient", "-t", token}
	if port != "" {
		args = append(args, "-p", port)
	}
	if name != "" {
		args = append(args, "--name", name)
	}
	result := runExeCommand(exePath, args...)
	if !result.Success && result.Output == "" {
		result.Output = "未捕获到详细输出(安装过程发生在提权后的独立进程里)。" +
			"请查看 " + installLogPath + " 了解具体失败原因。"
	}
	return result
}

// generateDiag 跑一次 `<exePath> diag`,成功后从输出里解析出生成的 zip 路径,
// 用 explorer.exe 打开它所在的文件夹并选中该文件,方便直接找到诊断包。
func generateDiag(exePath string) ActionResult {
	result := runExeCommand(exePath, "diag")
	if result.Success {
		if path := parseDiagPath(result.Output); path != "" {
			_ = exec.Command("explorer.exe", "/select,", path).Start()
		}
	}
	return result
}

// testConnectivity 依次 ping 配置好的每个目标 IP(-n 3,和之前面板里手填 IP 时
// 用的参数一样),把每个目标的结果拼在一起返回。任一目标 ping 通就算整体
// Success——目的是"确认到内网至少还有一条路通",不是要求每个目标都必须通。
// targets 为空(理论上不该发生,main.go 应该总是配好至少一个)时返回明确的
// 错误说明,而不是静默地什么都不做。
func testConnectivity(targets []string) ActionResult {
	if len(targets) == 0 {
		return ActionResult{Success: false, Output: "未配置测试目标 IP,请在面板的\"设置\"里添加"}
	}
	var lines []string
	anyOK := false
	for _, ip := range targets {
		out, err := hiddenCommand("ping.exe", "-n", "3", ip).CombinedOutput()
		ok := err == nil
		if ok {
			anyOK = true
		}
		status := "失败"
		if ok {
			status = "成功"
		}
		lines = append(lines, ip+" — "+status)
		lines = append(lines, strings.TrimSpace(decodeConsoleOutput(out)))
	}
	return ActionResult{Success: anyOK, Output: strings.Join(lines, "\n")}
}

// saveSettings 把去掉首尾空白、丢弃空字符串之后的 targets 写回 settingsPath。
// 至少要留一个非空 IP——全部清空会让"测试连通性"永远失败且看不出原因,不如
// 直接拒绝保存、把问题留在设置界面上让用户看得到。
func saveSettings(settingsPath string, targets []string) ActionResult {
	cleaned := make([]string, 0, len(targets))
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t != "" {
			cleaned = append(cleaned, t)
		}
	}
	if len(cleaned) == 0 {
		return ActionResult{Success: false, Output: "至少需要保留一个 IP"}
	}
	data, err := json.MarshalIndent(settings.Settings{ConnectivityTargets: cleaned}, "", "  ")
	if err != nil {
		return ActionResult{Success: false, Output: "序列化设置失败: " + err.Error()}
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return ActionResult{Success: false, Output: "写入 " + settingsPath + " 失败: " + err.Error()}
	}
	return ActionResult{Success: true, Output: "已保存 " + strings.Join(cleaned, ", ")}
}

// parseDiagPath 从 `diag` 子命令的输出里取出 "diag bundle written to <path>"
// 这一行后面的路径;解析不出来就返回空字符串。
func parseDiagPath(output string) string {
	const marker = "diag bundle written to "
	idx := strings.Index(output, marker)
	if idx == -1 {
		return ""
	}
	rest := output[idx+len(marker):]
	if nl := strings.IndexAny(rest, "\r\n"); nl != -1 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
}
