// Package selfupdate 实现 NetSentry 自身的自动升级:从管理员配置的内网镜像地址
// 拉取版本清单,发现新版本就下载、校验 SHA256、替换安装目录里的两个 exe。
//
// 设计要点:
//   - 升级源(UpdateBaseURL)存在 settings.json 里,由管理员配置/镜像,不依赖
//     GitHub——正是因为 GitHub 在内网环境下载太慢/不稳定才做的这个功能。
//   - 检查由 watch 巡检(每 5 分钟)触发,内部用时间戳文件节流为每小时最多一次
//     ——发布后全部客户端最迟 1 小时内升级,而清单文件很小,不会给镜像造成压力。
//   - 镜像目录按版本号分目录存放 exe(见 selfupdate_windows.go 里下载路径的注释)。
//   - 清单必须带 ed25519 签名且只升不降(见 UpdatePublicKeyHex / IsNewerVersion),
//     回滚 = 用旧代码发一个更高的版本号(scripts/release.sh 一条命令)。
//   - 替换正在运行的 exe 用 Windows 的标准手法:正在运行的 exe 不能删除/覆盖,
//     但可以改名——先把当前 exe 改名成 .old,再把新文件放到原位;正在跑的进程
//     继续持有旧文件句柄不受影响,下次启动(计划任务下一轮、托盘重启、开机)
//     自然用上新版本。.old 文件在下一次升级检查时清理。
package selfupdate

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UpdatePublicKeyHex 是验签用的 ed25519 公钥。对应的私钥只存在发布者的开发机上
// (~/.config/netsentry/signing.key,由 cmd/signmanifest 生成、scripts/release.sh
// 使用),不在仓库里、也不在镜像服务器上——镜像被攻破也伪造不出合法清单,
// 自动升级通道不会变成远程执行任意代码的入口。
const UpdatePublicKeyHex = "35a96c9ff4c530f02d9f843fd72355136c8259d9fe4875c9abefb00b270c1f98"

// VerifyManifestSignature 用 hex 公钥验证 manifest 内容的 ed25519 签名(sigHex
// 是 version.json.sig 的内容)。任何一步不对都返回错误,调用方必须拒绝该清单。
func VerifyManifestSignature(manifest []byte, sigHex, publicKeyHex string) error {
	pub, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("公钥格式不合法")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("version.json.sig 格式不合法")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), manifest, sig) {
		return fmt.Errorf("version.json 签名校验失败,拒绝该清单")
	}
	return nil
}

// IsNewerVersion 判断 candidate 是否比 current 更新(两者都是 "x.y.z" 数字版本
// 号)。解析不了就报错,调用方按"不升级"处理——防降级:攻击者重放旧的已签名
// 清单最多让升级停摆,不能把全员退回有漏洞的旧版本。
func IsNewerVersion(candidate, current string) (bool, error) {
	c, err := parseVersion(candidate)
	if err != nil {
		return false, err
	}
	cur, err := parseVersion(current)
	if err != nil {
		return false, err
	}
	for i := range c {
		if c[i] != cur[i] {
			return c[i] > cur[i], nil
		}
	}
	return false, nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("无法解析版本号 %q", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, fmt.Errorf("无法解析版本号 %q", v)
		}
		out[i] = n
	}
	return out, nil
}

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
