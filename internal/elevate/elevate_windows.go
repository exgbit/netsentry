//go:build windows

package elevate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// IsElevated 判断当前进程是否以管理员权限运行。
//
// 用 `net session` 探测:该命令在管理员权限下能成功执行(不要求真的存在活动会话),
// 非管理员权限下会因权限不足失败(报"系统错误 5:拒绝访问",退出码非零)——这是一个
// 广泛使用的、不需要额外依赖的管理员权限检测技巧。这里只看退出码,不解析具体错误
// 文案(文案随系统语言变化,退出码不会)。
func IsElevated() (bool, error) {
	err := exec.Command("net.exe", "session").Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, err
}

// RelaunchElevated 用 UAC 提权重新启动当前程序(带上同样的命令行参数),
// 成功发起后调用方应该直接退出当前(非提权的)进程。
func RelaunchElevated(args []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable path: %w", err)
	}

	cmd := relaunchCommand(exePath, args)
	if out, err := exec.Command("powershell.exe", "-Command", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("powershell relaunch elevated: %w: %s", err, out)
	}
	return nil
}
