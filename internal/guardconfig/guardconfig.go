// Package guardconfig 负责解析 netclient.json / servers.json 并判断两者的身份 ID 是否一致。
//
// 背景:netclient 启动时会比较 netclient.json 里的 id 字段和 servers.json 里缓存的 mqid 字段,
// 不一致就 fatal 退出且不会自愈(源码见 gravitl/netclient cmd/root.go 的 checkConfig)。
// 这个包把该判断逻辑独立出来,供 backup/watch 复用。
package guardconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ParseNetclientID 从 netclient.json 的原始内容中提取本机身份 ID(id 字段)。
func ParseNetclientID(data []byte) (string, error) {
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("parse netclient.json: %w", err)
	}
	return v.ID, nil
}

// ParseServerMQIDs 从 servers.json 的原始内容中提取每个 server 条目的 name -> mqid 映射。
func ParseServerMQIDs(data []byte) (map[string]string, error) {
	var raw map[string]struct {
		MQID string `json:"mqid"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse servers.json: %w", err)
	}
	result := make(map[string]string, len(raw))
	for key, entry := range raw {
		name := entry.Name
		if name == "" {
			name = key
		}
		result[name] = entry.MQID
	}
	return result, nil
}

// IsConsistent 判断 netclient 的本机身份 ID 与所有已知 server 缓存的 mqid 是否一致。
// 没有任何 server 条目时视为不一致(说明还没加入任何网络,不该当作"健康"状态处理)。
func IsConsistent(netclientID string, mqids map[string]string) bool {
	if netclientID == "" || len(mqids) == 0 {
		return false
	}
	for _, mqid := range mqids {
		if mqid != netclientID {
			return false
		}
	}
	return true
}

// LoadResult 描述从磁盘读取 netclient.json / servers.json 后的状态。
type LoadResult struct {
	NetclientExists bool
	ServersExists   bool
	NetclientID     string
	ServerMQIDs     map[string]string
	Consistent      bool
}

// Load 从指定目录读取 netclient.json 和 servers.json 并做一致性判断。
// dir 通常是 C:\Program Files (x86)\Netclient\,测试时用临时目录替代。
func Load(dir string) (LoadResult, error) {
	var result LoadResult

	ncPath := filepath.Join(dir, "netclient.json")
	if data, err := os.ReadFile(ncPath); err == nil {
		result.NetclientExists = true
		id, err := ParseNetclientID(data)
		if err != nil {
			return result, err
		}
		result.NetclientID = id
	} else if !os.IsNotExist(err) {
		return result, err
	}

	srvPath := filepath.Join(dir, "servers.json")
	if data, err := os.ReadFile(srvPath); err == nil {
		result.ServersExists = true
		mqids, err := ParseServerMQIDs(data)
		if err != nil {
			return result, err
		}
		result.ServerMQIDs = mqids
	} else if !os.IsNotExist(err) {
		return result, err
	}

	result.Consistent = result.NetclientExists && result.ServersExists &&
		IsConsistent(result.NetclientID, result.ServerMQIDs)
	return result, nil
}
