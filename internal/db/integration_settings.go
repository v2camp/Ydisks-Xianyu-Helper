package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// userIntegrationSettingKeys 是本系统允许按本地用户保存的集成配置键。
// 这些键不再接受系统级写入；全局旧值由一次性数据迁移折叠到管理员用户。
var userIntegrationSettingKeys = map[string]struct{}{
	"ai_api_url":             {},
	"ai_base_url":            {},
	"ai_model":               {},
	"ai_api_key":             {},
	"smtp_server":            {},
	"smtp_port":              {},
	"smtp_user":              {},
	"smtp_password":          {},
	"smtp_from_name":         {},
	"smtp_from_address":      {},
	"smtp_use_tls":           {},
	"smtp_use_ssl":           {},
	"qqbot.app_id":           {},
	"qqbot.app_secret":       {},
	"qqbot.commands_enabled": {},
	"qqbot.command_openids":  {},
}

// IsUserIntegrationSettingKey 判断设置键是否属于用户级集成配置。
func IsUserIntegrationSettingKey(key string) bool {
	// ok 表示当前键是否属于用户级集成配置。
	_, ok := userIntegrationSettingKeys[strings.TrimSpace(key)]
	return ok
}

// UserIntegrationSettingKeys 返回全部用户级集成配置键；调用方不得修改返回切片。
func UserIntegrationSettingKeys() []string {
	// keys 保存用户级集成配置键列表。
	keys := make([]string, 0, len(userIntegrationSettingKeys))
	// key 是当前遍历到的用户级集成配置键。
	for key := range userIntegrationSettingKeys {
		keys = append(keys, key)
	}
	return keys
}

// ReadSensitiveUserSetting 读取用户级敏感设置，并在解密前写入访问审计。
// userID 必须是正数；key 必须属于敏感设置白名单且是用户集成键。
func (s *Store) ReadSensitiveUserSetting(ctx context.Context, userID int64, key, action, resource string) (string, error) {
	if s == nil || s.UserSettings == nil {
		return "", errors.New("用户设置存储未初始化")
	}
	if userID <= 0 {
		return "", ErrInvalidUserID
	}
	key = strings.TrimSpace(key)
	if !IsSensitiveSettingKey(key) || !IsUserIntegrationSettingKey(key) {
		return "", fmt.Errorf("设置 %q 不属于用户级敏感设置", key)
	}
	if strings.TrimSpace(action) == "" || strings.TrimSpace(resource) == "" {
		return "", errors.New("用户敏感设置审计上下文无效")
	}
	if s.SecurityAudit == nil {
		return "", errors.New("敏感设置访问审计未初始化")
	}
	// err 表示写入敏感设置访问审计失败。
	if err := s.SecurityAudit.Add(ctx, SecurityAuditLog{
		UserID: userID, Action: strings.TrimSpace(action), Resource: strings.TrimSpace(resource),
		Keys: []string{key}, Outcome: "accepted",
	}); err != nil {
		return "", fmt.Errorf("记录用户敏感设置访问审计失败: %w", err)
	}
	return s.UserSettings.GetForUser(ctx, userID, key)
}

// GetUserSettingForAccount 解析账号所有者后读取用户级设置；非敏感键与敏感键均可读取，缺失返回空字符串。
func (s *Store) GetUserSettingForAccount(ctx context.Context, cookieID, key string) (string, error) {
	if s == nil || s.Cookies == nil || s.UserSettings == nil {
		return "", errors.New("账号或用户设置存储未初始化")
	}
	// ownerID、err 是账号所有者与查询错误。
	ownerID, err := s.Cookies.GetOwnerID(ctx, strings.TrimSpace(cookieID))
	if err != nil {
		return "", fmt.Errorf("读取账号所有者失败: %w", err)
	}
	if IsSensitiveSettingKey(key) {
		return s.ReadSensitiveUserSetting(ctx, ownerID, key, "settings.use", "account")
	}
	return s.UserSettings.GetForUser(ctx, ownerID, key)
}
