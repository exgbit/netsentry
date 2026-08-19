//go:build windows

// Package winsvc 通过 sc.exe 控制 Windows 服务,实现 watch.ServiceController 接口。
// 用 sc.exe 而不是 golang.org/x/sys/windows/svc/mgr,是为了不给这个单文件小工具引入额外依赖。
package winsvc

import (
	"fmt"
	"os/exec"
	"strings"
)

// SCController 通过 sc.exe 控制指定名字的 Windows 服务。
type SCController struct {
	Name string
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
