package trayui

import (
	"os/exec"
	"strings"
)

// PanelConfig 是面板需要的全部依赖,由 main.go 组装后传给 ShowPanel。
type PanelConfig struct {
	ExePath        string // 已安装的 netsentry.exe 路径,面板里所有需要跑子命令的操作都 shell 到这个路径
	NetclientDir   string
	BackupDir      string
	InstallLogPath string // guardDir + "install.log",setupNetclient 失败且没有可展示输出时,指引用户去看这个文件
	Svc            interface{ IsRunning() (bool, error) }
	// ConnectivityTargets 是"测试连通性"按钮要 ping 的目标 IP 列表——这是内部
	// 企业工具,网段是固定的,不需要每次都让用户自己输入 IP(真机测试发现
	// window.prompt() 在 WebView2 里渲染很怪,而且对着一批不懂技术的同事来说
	// "还要自己填 IP" 也是不必要的操作门槛)。在 main.go 里配置,不在面板里填。
	ConnectivityTargets []string
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
	out, err := exec.Command(exePath, args...).CombinedOutput()
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
func setupNetclientResult(exePath, installLogPath, token string) ActionResult {
	result := runExeCommand(exePath, "setup-netclient", "-t", token)
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
		return ActionResult{Success: false, Output: "未配置测试目标 IP(联系管理员在 NetSentry 里配置 ConnectivityTargets)"}
	}
	var lines []string
	anyOK := false
	for _, ip := range targets {
		out, err := exec.Command("ping.exe", "-n", "3", ip).CombinedOutput()
		ok := err == nil
		if ok {
			anyOK = true
		}
		status := "失败"
		if ok {
			status = "成功"
		}
		lines = append(lines, ip+" — "+status)
		lines = append(lines, strings.TrimSpace(string(out)))
	}
	return ActionResult{Success: anyOK, Output: strings.Join(lines, "\n")}
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
