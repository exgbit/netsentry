//go:build darwin

package selfupdate

// manifestFile 是镜像上 macOS 版的清单文件名(与 Windows 的 version.json 分开,
// 原因见 selfupdate_windows.go)。清单里的文件 key 是 "netsentry"(universal
// 二进制),对应安装路径 /usr/local/bin/netsentry。
const manifestFile = "version-mac.json"

// cleanupPatterns 是每轮升级检查时尽力清理的历史残留文件模式。
var cleanupPatterns = []string{"netsentry.old-*", "netsentry.failed"}
