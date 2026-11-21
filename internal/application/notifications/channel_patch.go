package notifications

// applyChannelPatch 合并渠道部分更新并校验最终字段。
// record 是已通过归属查询的内部记录；Config 只在本包与持久化边界之间传递。
func applyChannelPatch(record *ChannelRecord, patch ChannelPatch) error {
	// existingType 保存更新前的渠道协议类型，用于判定本次配置提交是否需要按键合并。
	existingType := record.Type
	if patch.Name != nil {
		record.Name = *patch.Name
	}
	if patch.Type != nil {
		record.Type = *patch.Type
	}
	if patch.Config != nil {
		// spec 按更新前的渠道类型取配置键分类，避免本次同时改类型时误用新类型的合并规则。
		spec := channelConfigSpecFor(existingType)
		if spec.Merge && !patch.ReplaceConfig {
			// 编辑器不会回传敏感键，因此声明了 Merge 的类型必须按键合并，不能整体替换丢掉未提交的凭据。
			record.Config = mergeChannelConfig(record.Config, *patch.Config)
		} else {
			record.Config = *patch.Config
		}
	}
	if patch.EventTypes != nil {
		record.EventTypes = *patch.EventTypes
	}
	if patch.Enabled != nil {
		record.Enabled = *patch.Enabled
	}
	return validateChannelFields(record.Name, record.Type)
}
