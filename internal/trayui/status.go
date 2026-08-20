// Package trayui 负责给托盘图标/面板聚合状态信息(可测的部分);
// systray 图标绘制和 WebView2 面板渲染留给不可单测的 9b/9c 子任务。
package trayui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"netsentry/internal/guardconfig"
)

// Status 是面板/图标需要的全部信息,序列化成 JSON 传给前端。
type Status struct {
	Configured    bool   `json:"configured"` // netclient.json 是否存在(决定面板显示"首次配置表单"还是"仪表盘")
	Healthy       bool   `json:"healthy"`    // 配置一致 且 服务 Running(决定图标绿/红)
	ServerName    string `json:"serverName"`
	LastBackup    string `json:"lastBackup"`    // 读 backup\last-good.txt,读不到就空字符串
	ServiceStatus string `json:"serviceStatus"` // "Running"/"Stopped"/"Unknown"
	Version       string `json:"version"`       // NetSentry 自身版本号,由 bindPanel 填充,不属于 Collect 聚合的"配置健康"信息
}

// Collect 聚合 guardconfig.Load + 服务状态 + last-good.txt,得到当前状态。
// svc 是 watch.ServiceController(复用 Phase 1 已有接口,不新造一个),方便测试用假实现。
//
// 真机踩过的坑:guardconfig.Load 对"文件不存在"不算错误(只是把 NetclientExists
// 置 false),只有在文件存在但读取/解析真的失败时(比如被别的进程短暂占用—— 这个
// 面板每 3 秒轮询一次 getStatus,撞上这种瞬时失败的概率不低)才会返回非 nil
// error。最早的实现在这种情况下直接 `return Status{}, err`,Status{} 的零值
// Configured 是 false——JS 侧会把这个当成"从没配置过",把仪表盘换成"输入 token
// 安装"的表单,吓到已经配置好的用户以为整个配置被清空了。这里改成:只要
// guardconfig.Load 报错(说明文件是存在的,只是读取这一下没成功),就认定
// Configured 仍然是 true,只是这一轮状态不健康/未知,继续停留在仪表盘,不要
// 因为一次瞬时读取失败就把界面切回安装表单。
func Collect(netclientDir, backupDir string, svc interface{ IsRunning() (bool, error) }) (Status, error) {
	load, loadErr := guardconfig.Load(netclientDir)

	running, svcErr := svc.IsRunning()
	var serviceStatus string
	switch {
	case svcErr != nil:
		serviceStatus = "Unknown"
		running = false // 查询失败时视为"未运行",不当作健康
	case running:
		serviceStatus = "Running"
	default:
		serviceStatus = "Stopped"
	}

	lastBackup := ""
	if data, err := os.ReadFile(filepath.Join(backupDir, "last-good.txt")); err == nil {
		lastBackup = string(data)
	}

	if loadErr != nil {
		return Status{
			Configured:    true,
			Healthy:       false,
			ServerName:    "",
			LastBackup:    lastBackup,
			ServiceStatus: serviceStatus,
		}, nil
	}

	return Status{
		Configured:    load.NetclientExists,
		Healthy:       load.Consistent && running,
		ServerName:    serverName(load.ServerMQIDs),
		LastBackup:    lastBackup,
		ServiceStatus: serviceStatus,
	}, nil
}

// serverName 从 name -> mqid 映射中取展示用的 server 名字:通常只有一个 server,
// 直接用它的名字;一个都没有就留空;理论上有多个的话(计划文档未细说这种场景),
// 用逗号拼接全部展示出来,不丢信息。
func serverName(mqids map[string]string) string {
	names := make([]string, 0, len(mqids))
	for name := range mqids {
		names = append(names, name)
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		sort.Strings(names)
		return strings.Join(names, ",")
	}
}
