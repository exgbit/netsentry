// Package elevate 处理管理员权限检测和 UAC 自提权重启。
package elevate

import (
	"fmt"
	"strings"
)

// relaunchCommand 构造用于 UAC 提权重启自身的 powershell.exe -Command 参数。
// exePath 是当前可执行文件的绝对路径,args 是要透传给提权后进程的命令行参数
// (如 ["install"]、["uninstall", "--purge"])。
//
// -ArgumentList 支持逗号分隔的多个带引号字符串。调用方(main.go 的 install/uninstall)
// 传入的 args 来自 os.Args[1:],进到这里之前已经保证非空(至少有子命令名本身),
// 所以不需要特别处理"完全没有参数"这种情况——但为了这个纯函数本身的健壮性,
// args 为空时仍然正确地省略掉 -ArgumentList,而不是传一个空字符串参数
// (空字符串参数和"完全没有参数"对被启动的程序来说是不同的)。
//
// 用 -PassThru 拿到 Start-Process 返回的进程对象,再用 `exit $p.ExitCode` 让包装用的
// powershell.exe 自身以跟提权子进程相同的退出码退出——Start-Process -Wait 本身不会把
// 子进程的退出码透传给 powershell.exe 自己的退出码,不显式做这一步的话,提权子进程
// 内部失败(比如中途 os.Exit(1))在发起提权的这一端会被误判成成功。
//
// 整段 Start-Process 调用还包了一层 try/catch,并显式加了 -ErrorAction Stop:
// Start-Process 启动失败时(最典型的场景是用户在 UAC 弹窗上点了"否")默认只是个
// 非终止性错误,PowerShell 会继续往下跑到 `exit $p.ExitCode`——这时 $p 还是 $null,
// `$null.ExitCode` 是 $null,`exit $null` 会被当成 exit 0,提权彻底没发起也会被
// 误判成成功。-ErrorAction Stop 把这个失败转成终止性错误,被 catch 捕获后显式
// exit 1,让"UAC 被拒绝/启动失败"和"提权进程内部失败"一样,都能让包装用的
// powershell.exe 以非零码退出。
//
// exePath 和每个 arg 都会先经过 escapePSSingleQuoted 转义再包进单引号:exePath
// 目前始终来自编译期固定的路径,args 以前也都是 main.go 里写死的子命令名
// (["install"]、["uninstall", "--purge"]),本来风险为零;但 9c 的面板把 setup-netclient
// 的 token(自由文本、来自面板输入框)接到了这条链路上(setupNetclient(token) ->
// `<exe> setup-netclient -t <token>` -> 该子进程自己的 ensureElevated() -> 这里),
// token 里如果带一个单引号就会提前把 PowerShell 单引号字符串截断,轻则搞坏这次
// relaunch,重则是注入的雏形——所以两处都要转义,不只转义 token 那一个。
func relaunchCommand(exePath string, args []string) string {
	cmd := fmt.Sprintf("$p = Start-Process -FilePath '%s'", escapePSSingleQuoted(exePath))
	if len(args) > 0 {
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = "'" + escapePSSingleQuoted(a) + "'"
		}
		cmd += " -ArgumentList " + strings.Join(quoted, ",")
	}
	cmd += " -Verb RunAs -Wait -PassThru -ErrorAction Stop; exit $p.ExitCode"
	return "try { " + cmd + " } catch { exit 1 }"
}

// escapePSSingleQuoted 转义字符串里的单引号,使其能安全地嵌进 PowerShell 的
// 单引号字符串字面量——PowerShell 里单引号字符串内转义单引号的写法是把它写两遍
// (一个单引号变成两个连续的单引号),不是反斜杠转义。
func escapePSSingleQuoted(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
