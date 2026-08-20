// Package settings 管理 NetSentry 自己的、IT 管理员部署后可以手动调整的配置项
// (目前只有测试连通性用的目标 IP 列表)。和 internal/guardconfig 不是一回事——
// guardconfig 读的是 netclient 自己的 netclient.json/servers.json,只读、不属于
// 本工具管理;这里管理的是本工具自己的设置,允许被管理员编辑。
package settings

import (
	"encoding/json"
	"os"
)

// Settings 是配置文件的 JSON 结构。
type Settings struct {
	// ConnectivityTargets 是面板"测试连通性"按钮要 ping 的目标 IP 列表——最早
	// 硬编码在源码里("改了要重新编译发布"),对要把这个工具分发给同事的内部
	// IT 场景不现实,换成配置文件让管理员按每个部署环境自己调整。
	ConnectivityTargets []string `json:"connectivityTargets"`
}

// Default 是没有配置文件、或配置文件里某个字段留空时用的默认值。
func Default() Settings {
	return Settings{ConnectivityTargets: []string{"100.67.147.4"}}
}

// Load 读取 path 处的 JSON 配置文件。
//
// 文件不存在时返回 Default()、不算错误(和 guardconfig.Load 对"文件不存在"的
// 处理方式一致)。文件存在但 JSON 解析失败,或 ConnectivityTargets 是空列表,
// 都回退到默认值——分别对应"管理员手改配置文件时手滑打错格式"和"故意留空"
// 两种情况,都不该让测试连通性功能直接不可用。
func Load(path string) (Settings, error) {
	def := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return def, nil
		}
		return def, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return def, err
	}
	if len(s.ConnectivityTargets) == 0 {
		s.ConnectivityTargets = def.ConnectivityTargets
	}
	return s, nil
}

// WriteDefaultIfMissing 在 path 不存在时写入一份默认配置文件,方便管理员发现
// "原来这里能改"、照着已有格式改。path 已存在时不覆盖——保护管理员已经手动
// 调整过的内容不会被 install/reinstall 冲掉。
func WriteDefaultIfMissing(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
