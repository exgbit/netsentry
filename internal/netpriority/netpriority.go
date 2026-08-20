// Package netpriority 处理 netclient 创建的 "netmaker" 虚拟网卡的接口跃点数
// (InterfaceMetric)。
//
// 真实反馈的问题:netclient 装好并加入网络之后,这块虚拟网卡的跃点数经常比
// 本机正常的以太网/Wi-Fi 网卡还低,Windows 做路由/DNS 服务器选择时会优先走
// 跃点数更低的接口——结果就是所有域名解析都要先绕一圈公司内网那台 VPN 服务器,
// 网页打开慢得离谱。手动修复方式是管理员权限跑
// `Set-NetIPInterface -InterfaceAlias "netmaker" -InterfaceMetric 100`
// 把跃点数调高,让 Windows 只在真正需要访问 VPN 网段时才选它。这个包把这件事
// 自动化,跑在 watch 的每一轮巡检里。
package netpriority

import (
	"strconv"
	"strings"
)

const (
	interfaceAlias = "netmaker"
	targetMetric   = 100
)

// Result 描述一次 Fix 调用的结果。
type Result struct {
	Applied bool // 是否真的执行了修复(接口存在且原本跃点数过低)
	Detail  string
}

// parseMetrics 从 `Get-NetIPInterface ... | Select-Object -ExpandProperty
// InterfaceMetric` 的原始输出里解析出跃点数列表——同一个接口别名通常会有
// IPv4/IPv6 两条记录,每行一个数字。
func parseMetrics(text string) []int {
	var metrics []int
	for _, field := range strings.Fields(text) {
		if m, err := strconv.Atoi(field); err == nil {
			metrics = append(metrics, m)
		}
	}
	return metrics
}

// needsFix 判断这组跃点数里是不是至少有一个低于 targetMetric——只要有一个
// (通常是 IPv4 那条)过低,对应地址族的流量/DNS 就会被这块网卡抢走优先级。
// metrics 为空(接口不存在)时不需要修复。
func needsFix(metrics []int) (needs bool, min int) {
	if len(metrics) == 0 {
		return false, 0
	}
	min = metrics[0]
	for _, m := range metrics {
		if m < min {
			min = m
		}
		if m < targetMetric {
			needs = true
		}
	}
	return needs, min
}
