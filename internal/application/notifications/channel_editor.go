package notifications

import (
	"encoding/json"
	"strings"
)

// channelEditorFromRecord 把内部渠道记录转换为剔除敏感值的编辑器视图。
// record.Config 只在本函数内解析；任何凭据值都不会进入返回模型。
func channelEditorFromRecord(record *ChannelRecord) ChannelEditor {
	// editor 保存剔除敏感配置后的编辑器视图。
	editor := ChannelEditor{ID: record.ID, Name: record.Name, Type: record.Type, EventTypes: record.EventTypes, Enabled: record.Enabled}
	// config 保存仅为回显非敏感字段而解析的渠道配置；解析失败时按空对象处理，不向响应输出任何配置原文。
	var config map[string]any
	if json.Unmarshal([]byte(record.Config), &config) != nil {
		config = nil
	}
	// spec 保存当前渠道类型的配置键分类；record.Type 本身保持存储原值，仅在查表时归一别名。
	spec := channelConfigSpecFor(record.Type)
	// 通用的可回显键值回读：只有 spec 声明的非敏感键才可能进入 Config，敏感键的值不会出现在这里。
	editor.Config = editableChannelConfig(spec, config)
	// 敏感键只回读键名，供前端区分「已保存凭据」与「改用连接器凭据」，永不回传值。
	editor.SavedCredentialKeys = savedChannelCredentialKeys(spec, config)
	if record.Type != "email" {
		return editor
	}
	// recipient 保存兼容 to_email/email 两种历史字段后的收件地址。
	recipient := strings.TrimSpace(stringValue(config["to_email"]))
	if recipient == "" {
		recipient = strings.TrimSpace(stringValue(config["email"]))
	}
	editor.ToEmail = recipient
	return editor
}
