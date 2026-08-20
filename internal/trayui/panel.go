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
