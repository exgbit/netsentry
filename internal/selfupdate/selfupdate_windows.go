//go:build windows

package selfupdate

// manifestFile 是镜像上 Windows 版的清单文件名。macOS 版是独立的清单
// (见 selfupdate_darwin.go)——同一份清单里混两个平台的文件不行:客户端会
// 下载清单里列出的全部文件,Windows 机器不该被塞进一个 mach-O 二进制。
const manifestFile = "version.json"

// cleanupPatterns 是每轮升级检查时尽力清理的历史残留文件模式。
var cleanupPatterns = []string{"*.exe.old*", "*.exe.failed"}
