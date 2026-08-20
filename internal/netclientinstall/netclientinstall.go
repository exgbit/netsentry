// Package netclientinstall 负责下载 netclient 本体、安装并加入网络
// (setup-netclient 子命令的核心逻辑)。
package netclientinstall

import "fmt"

// PinnedVersion 是团队验证过的稳定 netclient 版本,编译时常量,升级 = 改这个值重新编译发布。
const PinnedVersion = "v1.6.0"

// DownloadURL 构造指定版本 netclient Windows 安装包的下载地址。
//
// 真机反馈过:GitHub Releases(github.com/gravitl/netclient)在这边网络环境下
// 经常连不上、下载直接超时失败。改用 downloads.netmaker.io——这是用户内部
// 实际验证过能用的下载地址,只有版本号这一段会变。
func DownloadURL(version string) string {
	return fmt.Sprintf("https://downloads.netmaker.io/releases/download/%s/netclientbundle.exe", version)
}
