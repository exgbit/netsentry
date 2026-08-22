// Package selfupdate 实现 NetSentry 自身的自动升级:从管理员配置的内网镜像地址
// 拉取版本清单,发现新版本就下载、校验 SHA256、替换安装目录里的两个 exe。
//
// 设计要点:
//   - 升级源(UpdateBaseURL)存在 settings.json 里,由管理员配置/镜像,不依赖
//     GitHub——正是因为 GitHub 在内网环境下载太慢/不稳定才做的这个功能。
//   - 检查由 watch 巡检(每 5 分钟)触发,内部用时间戳文件节流为每小时最多一次
//     ——发布后全部客户端最迟 1 小时内升级,而清单文件很小,不会给镜像造成压力。
//   - 镜像目录按版本号分目录存放 exe(见 selfupdate_windows.go 里下载路径的注释),
//     回滚 = 把镜像上的 version.json 改回旧版本号。
//   - 替换正在运行的 exe 用 Windows 的标准手法:正在运行的 exe 不能删除/覆盖,
//     但可以改名——先把当前 exe 改名成 .old,再把新文件放到原位;正在跑的进程
//     继续持有旧文件句柄不受影响,下次启动(计划任务下一轮、托盘重启、开机)
//     自然用上新版本。.old 文件在下一次升级检查时清理。
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Manifest 是镜像上 version.json 的结构。Files 的 key 是文件名(netsentry.exe /
// netsentry-tray.exe),value 是对应文件的 SHA256(hex)。
type Manifest struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

// ParseManifest 解析并校验 version.json 内容。
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse version.json: %w", err)
	}
	if m.Version == "" {
		return Manifest{}, fmt.Errorf("version.json missing version")
	}
	if len(m.Files) == 0 {
		return Manifest{}, fmt.Errorf("version.json missing files")
	}
	for name, sum := range m.Files {
		if len(sum) != 64 {
			return Manifest{}, fmt.Errorf("version.json: %s has invalid sha256 %q", name, sum)
		}
	}
	return m, nil
}

// ShouldCheck 根据上次检查时间戳(unix 秒,文件内容)决定这一轮要不要真的去
// 检查更新。stampContent 为空(文件不存在/读不到)视为从未检查过。
func ShouldCheck(stampContent string, now time.Time, interval time.Duration) bool {
	ts, err := strconv.ParseInt(strings.TrimSpace(stampContent), 10, 64)
	if err != nil {
		return true
	}
	return now.Sub(time.Unix(ts, 0)) >= interval
}

// SHA256Hex 计算字节内容的 SHA256 十六进制串。
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
