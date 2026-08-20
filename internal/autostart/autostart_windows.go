//go:build windows

package autostart

import (
	"fmt"
	"os/exec"
	"strings"
)

// Register 把 tray 加进当前用户登录启动项(exePath 是已安装到位的
// netsentry-tray.exe 路径)。幂等:reg add 带 /f 强制覆盖,重复调用安全。
func Register(exePath string) error {
	args := RegisterArgs(exePath)
	if out, err := exec.Command("reg.exe", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("reg %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}

// Unregister 删除该启动项。值本来就不存在时不视为错误,uninstall 要能在
// 部分安装/重复卸载的情况下正常跑完,不半途而废。
func Unregister() error {
	args := UnregisterArgs()
	out, err := exec.Command("reg.exe", args...).CombinedOutput()
	if err != nil && !isValueNotFoundOutput(string(out)) {
		return fmt.Errorf("reg %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}
