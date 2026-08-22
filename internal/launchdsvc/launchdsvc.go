// Package launchdsvc 是 macOS 上通过 launchctl 控制 LaunchDaemon 的服务控制器,
// 与 Windows 的 internal/winsvc(sc.exe)对应,实现 watch.ServiceController 的
// 同一组方法。所有操作都要求进程以 root 运行(system domain)。
package launchdsvc

import "strings"

// ParseRunning 从 `launchctl print system/<label>` 的输出判断服务是否在运行。
// 输出里有 "state = running" 一行表示进程活着;其他状态(waiting 等)或没有
// state 行都算没在运行。独立成纯函数方便单测。
func ParseRunning(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "state = ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "state = ")) == "running"
		}
	}
	return false
}
