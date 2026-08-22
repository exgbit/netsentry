// Package netclientinstall 负责下载 netclient 本体、安装并加入网络
// (setup-netclient 子命令的核心逻辑)。
package netclientinstall

import "fmt"

// PinnedVersion 是团队验证过的稳定 netclient 版本,编译时常量,升级 = 改这个值重新编译发布。
const PinnedVersion = "v1.6.0"

// DownloadURL 构造指定版本 netclient Windows CLI 二进制的下载地址。
//
// 真机反馈过:GitHub Releases(github.com/gravitl/netclient)在这边网络环境下
// 经常连不上、下载直接超时失败。改用 downloads.netmaker.io 镜像,只有版本号
// 这一段会变。
//
// 文件名必须是 netclient-windows-amd64.exe(纯 CLI 二进制),不能用同目录下的
// netclientbundle.exe——后者是带 GUI 的图形安装包,真机踩过坑:通过 SSH/隐藏
// 窗口跑 `netclientbundle.exe install` 时,它解压完文件后卡在一个没人看得见的
// 交互界面上,零 CPU 无限等待(9 分钟无进展,服务没注册、join 根本没执行),
// 整个 setup-netclient 就跟着挂死。CLI 二进制的 install/join 子命令才是本包
// 代码假设的语义。
func DownloadURL(version string) string {
	return fmt.Sprintf("https://downloads.netmaker.io/releases/download/%s/netclient-windows-amd64.exe", version)
}

// ExistingAction 是 setup-netclient 面对"本机可能已经装过 netclient"时的决定。
type ExistingAction int

const (
	// FreshInstall:本机没装过,正常走下载→安装→加入网络。
	FreshInstall ExistingAction = iota
	// KeepAndGuard:已装且配置正常,跳过重装(不消耗 enrollment key 次数、
	// 不中断现有隧道),直接给它开启 NetSentry 守护。
	KeepAndGuard
	// WipeAndReinstall:已装但配置不正常(netclient.json/servers.json 不一致、
	// 读不出来,或服务根本没注册),修不如重来——完整卸载后重新安装配置。
	WipeAndReinstall
)

// DecideExisting 根据三个事实做决定:netclient.exe 是否存在、配置是否一致
// (guardconfig.Load 的 Consistent)、netclient 服务是否已注册(sc query 是否
// 成功,不要求正在运行——服务注册了但停着,交给守护巡检去拉起即可)。
func DecideExisting(exeExists, configConsistent, serviceExists bool) ExistingAction {
	if !exeExists {
		return FreshInstall
	}
	if configConsistent && serviceExists {
		return KeepAndGuard
	}
	return WipeAndReinstall
}
