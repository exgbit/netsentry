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
func relaunchCommand(exePath string, args []string) string {
	cmd := fmt.Sprintf("Start-Process -FilePath '%s'", exePath)
	if len(args) > 0 {
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = "'" + a + "'"
		}
		cmd += " -ArgumentList " + strings.Join(quoted, ",")
	}
	cmd += " -Verb RunAs -Wait"
	return cmd
}
