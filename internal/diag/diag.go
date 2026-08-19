// Package diag 负责生成可以安全对外分享的诊断包:脱敏敏感字段后打包成 zip。
package diag

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
)

var allowedNetclientFields = map[string]bool{
	"id": true, "version": true, "os": true, "os_version": true,
	"interface": true, "name": true, "nodes": true, "endpointip": true,
	"created_at": true, "updated_at": true,
}

// SanitizeNetclientJSON 从 netclient.json 原始内容中只保留白名单字段,剔除私钥/密码等敏感信息。
// 用白名单而不是黑名单,是为了防止未来 netclient 新增未知的敏感字段时被漏掉。
func SanitizeNetclientJSON(data []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse netclient.json: %w", err)
	}
	clean := make(map[string]json.RawMessage)
	for k, v := range raw {
		if allowedNetclientFields[k] {
			clean[k] = v
		}
	}
	return json.MarshalIndent(clean, "", "  ")
}

var allowedServerFields = map[string]bool{
	"mqid": true, "name": true,
}

// SanitizeServersJSON 对 servers.json 里每个 server 条目做同样的白名单过滤。
func SanitizeServersJSON(data []byte) ([]byte, error) {
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse servers.json: %w", err)
	}
	clean := make(map[string]map[string]json.RawMessage, len(raw))
	for server, fields := range raw {
		cleanFields := make(map[string]json.RawMessage)
		for k, v := range fields {
			if allowedServerFields[k] {
				cleanFields[k] = v
			}
		}
		clean[server] = cleanFields
	}
	return json.MarshalIndent(clean, "", "  ")
}

// Source 是要写入诊断 zip 的一个文件条目。
type Source struct {
	Name string // zip 内的路径,比如 "guard.log" 或 "config-summary/netclient.json"
	Data []byte
}

// Bundle 把一组已经收集好、已脱敏的诊断内容打包成一个 zip 文件。
// 调用方负责在传入前完成脱敏(SanitizeNetclientJSON/SanitizeServersJSON)和真实系统状态的采集。
func Bundle(sources []Source, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create diag zip: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, s := range sources {
		w, err := zw.Create(s.Name)
		if err != nil {
			return fmt.Errorf("add %s to zip: %w", s.Name, err)
		}
		if _, err := w.Write(s.Data); err != nil {
			return fmt.Errorf("write %s to zip: %w", s.Name, err)
		}
	}
	return zw.Close()
}
