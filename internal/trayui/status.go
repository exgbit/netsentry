// Package trayui 负责给托盘图标/面板聚合状态信息(可测的部分);
// systray 图标绘制和 WebView2 面板渲染留给不可单测的 9b/9c 子任务。
package trayui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"netclient-guard/internal/guardconfig"
)

// Status 是面板/图标需要的全部信息,序列化成 JSON 传给前端。
type Status struct {
	Configured    bool   `json:"configured"` // netclient.json 是否存在(决定面板显示"首次配置表单"还是"仪表盘")
	Healthy       bool   `json:"healthy"`    // 配置一致 且 服务 Running(决定图标绿/红)
	ServerName    string `json:"serverName"`
	LastBackup    string `json:"lastBackup"`    // 读 backup\last-good.txt,读不到就空字符串
	ServiceStatus string `json:"serviceStatus"` // "Running"/"Stopped"/"Unknown"
}

// Collect 聚合 guardconfig.Load + 服务状态 + last-good.txt,得到当前状态。
// svc 是 watch.ServiceController(复用 Phase 1 已有接口,不新造一个),方便测试用假实现。
func Collect(netclientDir, backupDir string, svc interface{ IsRunning() (bool, error) }) (Status, error) {
	load, err := guardconfig.Load(netclientDir)
	if err != nil {
		return Status{}, err
	}

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
