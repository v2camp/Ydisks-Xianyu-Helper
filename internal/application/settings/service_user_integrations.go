package settings

import (
	"context"
	"strings"
)

// GetUserIntegrations 读取当前用户的 AI、QQ 与 SMTP 集成配置；敏感键只返回 configured 标记。
func (s *Service) GetUserIntegrations(ctx context.Context, userID int64) (map[string]string, error) {
	// err 表示当前用户身份校验失败。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	// keys 是本次读取需要审计的敏感键名集合。
	keys := s.repository.SensitiveSettingKeys()
	// err 表示读取审计写入失败。
	if err := s.audit(ctx, AuditRecord{UserID: userID, Action: "settings.read", Resource: "user_integrations", Keys: keys}); err != nil {
		return nil, err
	}
	return s.repository.RedactedUser(ctx, userID)
}

// ApplyUserIntegrationChanges 校验并原子保存当前用户的集成配置。
func (s *Service) ApplyUserIntegrationChanges(ctx context.Context, userID int64, values map[string]string, secrets map[string]SecretChange) error {
	// err 表示当前用户身份校验失败。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	if len(values) == 0 && len(secrets) == 0 {
		return validationError("设置不能为空")
	}
	// keys 保存本次变更的键名，供审计与运行时重载使用。
	keys := make([]string, 0, len(values)+len(secrets))
	// key 是当前待校验的普通集成配置键。
	for key := range values {
		key = strings.TrimSpace(key)
		if !s.repository.IsUserIntegrationSettingKey(key) {
			return validationErrorf("%s 不是用户级集成配置", key)
		}
		if s.repository.IsSensitiveSettingKey(key) {
			return validationError("敏感设置必须放入 secrets 命令")
		}
		keys = append(keys, key)
	}
	// key、change 是当前待校验的敏感三态命令。
	for key, change := range secrets {
		key = strings.TrimSpace(key)
		if !s.repository.IsUserIntegrationSettingKey(key) || !s.repository.IsSensitiveSettingKey(key) {
			return validationError("敏感设置命令无效")
		}
		if !validSecretAction(change) || change.Action == "replace" && strings.TrimSpace(change.Value) == "" {
			return validationError("敏感设置命令无效")
		}
		keys = append(keys, key)
	}
	// err 表示写入审计失败。
	if err := s.audit(ctx, AuditRecord{UserID: userID, Action: "settings.write", Resource: "user_integrations", Keys: keys}); err != nil {
		return err
	}
	// err 表示用户集成配置持久化失败。
	if err := s.repository.ApplyUserChanges(ctx, userID, values, secrets); err != nil {
		return err
	}
	s.invalidateCredentialCache()
	if s.userIntegrationListener != nil {
		s.userIntegrationListener.OnUserIntegrationChanged(ctx, userID, keys)
	}
	return nil
}
