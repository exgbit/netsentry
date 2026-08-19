// Package netclientinstall 负责下载 netclient 本体、安装并加入网络
// (setup-netclient 子命令的核心逻辑)。
package netclientinstall

import "fmt"

// PinnedVersion 是团队验证过的稳定 netclient 版本,编译时常量,升级 = 改这个值重新编译发布。
const PinnedVersion = "v1.6.0"

// DownloadURL 构造指定版本 netclient Windows 二进制的 GitHub Releases 下载地址。
func DownloadURL(version string) string {
	return fmt.Sprintf("https://github.com/gravitl/netclient/releases/download/%s/netclient-windows-amd64.exe", version)
}
