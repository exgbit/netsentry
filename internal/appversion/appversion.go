// Package appversion 是 NetSentry 版本号的唯一来源:Windows 和 macOS 两个入口
// 二进制、Makefile 的清单生成、scripts/release.sh 的一致性检查都读这里。
// 发版只改这一个常量。
package appversion

// Guard 是当前 NetSentry 版本号(x.y.z 数字格式,自动升级的防降级比较依赖这个
// 格式,见 internal/selfupdate.IsNewerVersion)。
const Guard = "0.7.0"
