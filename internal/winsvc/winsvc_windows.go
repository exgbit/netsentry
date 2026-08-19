//go:build windows

// Package winsvc 通过 sc.exe 控制 Windows 服务,实现 watch.ServiceController 接口。
// 用 sc.exe 而不是 golang.org/x/sys/windows/svc/mgr,是为了不给这个单文件小工具引入额外依赖。
package winsvc

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// SCController 通过 sc.exe 控制指定名字的 Windows 服务。
type SCController struct {
	Name    string
	LogPath string
}

func (c SCController) IsRunning() (bool, error) {
	out, err := exec.Command("sc.exe", "query", c.Name).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("sc query %s: %w: %s", c.Name, err, out)
	}
	return strings.Contains(string(out), "RUNNING"), nil
}

func (c SCController) Start() error {
	out, err := exec.Command("sc.exe", "start", c.Name).CombinedOutput()
	// 1056 = 服务已经在运行,视为成功
	if err != nil && !strings.Contains(string(out), "1056") {
		return fmt.Errorf("sc start %s: %w: %s", c.Name, err, out)
	}
	return nil
}

func (c SCController) Stop() error {
	out, err := exec.Command("sc.exe", "stop", c.Name).CombinedOutput()
	// 1062 = 服务尚未启动,视为成功
	if err != nil && !strings.Contains(string(out), "1062") {
		return fmt.Errorf("sc stop %s: %w: %s", c.Name, err, out)
	}
	return nil
}

func (c SCController) LogSize() (int64, error) {
	info, err := os.Stat(c.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", c.LogPath, err)
	}
	return info.Size(), nil
}

// ReadLogFrom 假设 winsw.out.log 是纯追加写入,不会被截断或轮转——netclient 自己生成的
// WinSW 服务配置(daemon/common_windows.go 的 writeServiceConfig)显式设置了 <log mode="append" />,
// 所以 offset 不会因为日志轮转而失效。如果这个前提以后变了,这里需要重新考虑。
//
// 注意:本实现假设 Windows 允许在其他进程(WinSW/netclient)持有写句柄时以只读方式打开这个文件。
// Go 的 os.Open 在 Windows 上默认请求较宽松的共享标志,预期能正常工作,但这个假设尚未在真实
// Windows 机器上针对一个正在被写入的 winsw.out.log 实测验证过。
func (c SCController) ReadLogFrom(offset int64) ([]byte, error) {
	f, err := os.Open(c.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", c.LogPath, err)
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek %s: %w", c.LogPath, err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.LogPath, err)
	}
	return data, nil
}
