//go:build windows

package netpriority

import (
	"fmt"

	"netsentry/internal/winexec"
)

// Fix 检查 netmaker 接口的跃点数,发现低于 targetMetric 就调高到 targetMetric。
// 接口不存在(netclient 还没装、没加入网络,或者刚 join 完接口还没来得及建立)
// 时不算错误,原样跳过。
//
// 真机验证过一个反直觉的坑:接口不存在时,哪怕加了 -ErrorAction
// SilentlyContinue 抑制了错误输出,包装用的 powershell.exe 进程本身依然会以
// 退出码 1 结束(在 -Command 这种一次性脚本模式下,是否有 error record 写入过
// 会影响进程退出码,和 -ErrorAction 有没有真的抑制住输出是两回事)。所以这里
// 不能像别处那样把非 nil err 当成硬失败——用"有没有解析出数字"而不是退出码
// 来判断接口是否存在,退出码本身不可靠。setup-netclient 里在 join 刚完成那一刻
// 调用这个函数时,接口很可能还没建立好,这种情况下不该报 WARN 吓用户,后面
// 常规巡检(watch)会在几分钟内自动重试补上。
func Fix() (Result, error) {
	queryOut, _ := winexec.Hidden("powershell.exe", "-NoProfile", "-Command",
		"Get-NetIPInterface -InterfaceAlias '"+interfaceAlias+"' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty InterfaceMetric",
	).CombinedOutput()

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
