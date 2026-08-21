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
