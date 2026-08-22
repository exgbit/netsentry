//go:build darwin

package launchdsvc

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Controller 控制一个 system domain 的 LaunchDaemon。
type Controller struct {
	Label     string // 如 "com.gravitl.netclient"
	PlistPath string // 如 "/Library/LaunchDaemons/com.gravitl.netclient.plist"
	LogPath   string // daemon 的 stdout/stderr 日志路径,IsBrokerStuck 检测要读
}

// IsRunning 报告 daemon 进程是否在运行。plist 没挂载(launchctl print 失败)
// 返回 error——与 winsvc 里"服务未注册时 sc query 报错"的语义对齐,调用方
// (watch.Decide / DecideExisting)依赖这个区分。
func (c Controller) IsRunning() (bool, error) {
	out, err := exec.Command("launchctl", "print", "system/"+c.Label).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("launchctl print system/%s: %w: %s", c.Label, err, strings.TrimSpace(string(out)))
	}
	return ParseRunning(string(out)), nil
}

// Start 确保 daemon 挂载并运行:plist 没挂载先 bootstrap(对应"用户在系统设置
// 里关掉了后台项/大版本升级重置"这类失效场景),已挂载则 kickstart 拉起。
// KeepAlive 的 daemon 平时由 launchd 自己保活,这里只兜底。
func (c Controller) Start() error {
	if _, err := c.IsRunning(); err != nil {
		if out, err := exec.Command("launchctl", "bootstrap", "system", c.PlistPath).CombinedOutput(); err != nil {
			return fmt.Errorf("launchctl bootstrap %s: %w: %s", c.PlistPath, err, strings.TrimSpace(string(out)))
		}
	}
	if out, err := exec.Command("launchctl", "kickstart", "system/"+c.Label).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart system/%s: %w: %s", c.Label, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Stop 把 daemon 从 launchd 卸载(bootout)——netclient 的 plist 是 KeepAlive
// 的,只 kill 进程会被 launchd 立刻拉起来,恢复配置期间必须先整个卸载,恢复完
// 由 Start 重新 bootstrap。plist 本来就没挂载不算错误。
func (c Controller) Stop() error {
	out, err := exec.Command("launchctl", "bootout", "system/"+c.Label).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "No such process") {
		return fmt.Errorf("launchctl bootout system/%s: %w: %s", c.Label, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// LogSize 返回 daemon 日志当前大小;日志不存在返回 0(刚安装还没写过日志)。
func (c Controller) LogSize() (int64, error) {
	info, err := os.Stat(c.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

// ReadLogFrom 读取日志从 offset 到末尾的内容;日志不存在返回空。
func (c Controller) ReadLogFrom(offset int64) ([]byte, error) {
	f, err := os.Open(c.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}
