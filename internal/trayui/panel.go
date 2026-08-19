package trayui

import (
	"os/exec"
	"strings"
)

// PanelConfig 是面板需要的全部依赖,由 main.go 组装后传给 ShowPanel。
type PanelConfig struct {
	ExePath      string // 已安装的 netclient-guard.exe 路径,面板里所有需要跑子命令的操作都 shell 到这个路径
	NetclientDir string
	BackupDir    string
	Svc          interface{ IsRunning() (bool, error) }
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
// 到已安装的 netclient-guard.exe 重新走一遍 CLI 子命令,复用同一套已经测试过的
// 代码路径,并且让 setup-netclient 自己的 ensureElevated() 正确处理提权边界。
func runExeCommand(exePath string, args ...string) ActionResult {
	out, err := exec.Command(exePath, args...).CombinedOutput()
	return ActionResult{Success: err == nil, Output: strings.TrimSpace(string(out))}
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
