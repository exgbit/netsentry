//go:build windows

package netpriority

import (
	"fmt"

	"netsentry/internal/winexec"
)

// Fix 检查 netmaker 接口的跃点数,发现低于 targetMetric 就调高到 targetMetric。
// 接口不存在(netclient 还没装,或没有加入任何网络)时不算错误,原样跳过。
//
// -ErrorAction SilentlyContinue:接口不存在时 Get-NetIPInterface 默认会抛一个
// 终止性错误,这里不希望"没装 netclient"被当成本函数的失败,所以吞掉这个错误、
// 靠"输出是不是空"来判断接口存不存在。
func Fix() (Result, error) {
	queryOut, err := winexec.Hidden("powershell.exe", "-NoProfile", "-Command",
		"Get-NetIPInterface -InterfaceAlias '"+interfaceAlias+"' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty InterfaceMetric",
	).CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("query %s interface metric: %w: %s", interfaceAlias, err, winexec.DecodeConsoleOutput(queryOut))
	}

	metrics := parseMetrics(winexec.DecodeConsoleOutput(queryOut))
	if len(metrics) == 0 {
		return Result{Detail: interfaceAlias + " 接口不存在,跳过"}, nil
	}

	fix, min := needsFix(metrics)
	if !fix {
		return Result{Detail: fmt.Sprintf("%s 接口跃点数已经是 %d,不需要调整", interfaceAlias, min)}, nil
	}

	setOut, err := winexec.Hidden("powershell.exe", "-NoProfile", "-Command",
		fmt.Sprintf("Set-NetIPInterface -InterfaceAlias '%s' -InterfaceMetric %d", interfaceAlias, targetMetric),
	).CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("set %s interface metric: %w: %s", interfaceAlias, err, winexec.DecodeConsoleOutput(setOut))
	}
	return Result{
		Applied: true,
		Detail:  fmt.Sprintf("%s 接口跃点数从 %d 调整为 %d,避免 DNS/路由被 VPN 抢占优先级", interfaceAlias, min, targetMetric),
	}, nil
}
