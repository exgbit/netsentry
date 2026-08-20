//go:build windows

// Package winsvc 通过 sc.exe 控制 Windows 服务,实现 watch.ServiceController 接口。
// 用 sc.exe 而不是 golang.org/x/sys/windows/svc/mgr,是为了不给这个单文件小工具引入额外依赖。
package winsvc

import (
	"fmt"
	"io"
	"os"
	"strings"

	"netsentry/internal/winexec"
)

// SCController 通过 sc.exe 控制指定名字的 Windows 服务。
type SCController struct {
	Name    string
	LogPath string
}

// 下面几个方法里 fmt.Errorf 拼错误消息时,原始 out 都先过一遍
// winexec.DecodeConsoleOutput 再塞进去——sc.exe 在中文 Windows 上是按系统 OEM
// 代码页(GBK)输出中文提示的,不转码直接当 UTF-8 用会显示成乱码,这条错误消息
// 最终可能透传到面板"立即修复"的执行结果里给用户看(真机上 ping.exe 走同一个
// 代码页问题真的复现过乱码,sc.exe 是同一类风险)。strings.Contains 那几处判断
// 只找 RUNNING/1056/1062 这类西文/数字 token,GBK 是 ASCII 兼容的,不转码去匹配
// 原始字节没问题,不用等解码。

func (c SCController) IsRunning() (bool, error) {
	out, err := winexec.Hidden("sc.exe", "query", c.Name).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("sc query %s: %w: %s", c.Name, err, winexec.DecodeConsoleOutput(out))
	}
	return strings.Contains(string(out), "RUNNING"), nil
}

func (c SCController) Start() error {
	out, err := winexec.Hidden("sc.exe", "start", c.Name).CombinedOutput()
	// 1056 = 服务已经在运行,视为成功
	if err != nil && !strings.Contains(string(out), "1056") {
		return fmt.Errorf("sc start %s: %w: %s", c.Name, err, winexec.DecodeConsoleOutput(out))
	}
	return nil
}

func (c SCController) Stop() error {
	out, err := winexec.Hidden("sc.exe", "stop", c.Name).CombinedOutput()
	// 1062 = 服务尚未启动,视为成功
	if err != nil && !strings.Contains(string(out), "1062") {
		return fmt.Errorf("sc stop %s: %w: %s", c.Name, err, winexec.DecodeConsoleOutput(out))
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
