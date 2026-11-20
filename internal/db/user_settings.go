package db

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

// UserSettings 保存用户级偏好与集成配置。普通键明文保存，敏感键用用户作用域加密。
type UserSettings struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

// userSecretOwner 返回用户级敏感设置的加密作用域所有者文本。
func userSecretOwner(userID int64) string {
	return strconv.FormatInt(userID, 10)
}

// AllForUser 查询指定用户的全部非敏感设置；敏感键不进入通用读取结果，避免误暴露。
func (s *UserSettings) AllForUser(ctx context.Context, userID int64) (map[string]string, error) {
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	// rows 和 err 是用户设置查询结果集及错误。
	rows, err := s.DB.QueryContext(ctx, `SELECT `+keyColumn+`, value FROM user_settings WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 是用户设置键值映射，敏感键只以 configured 标记出现。
	result := make(map[string]string)
	for rows.Next() {
		// key 和 value 是当前设置行的键和值。
		var key, value string
		// err 表示扫描用户设置行失败。
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if isSensitiveSettingKey(key) {
			result[key+"_configured"] = "true"
			continue
		}
		result[key] = value
	}
	return result, rows.Err()
}

// GetForUser 查询用户单项设置；敏感键解密后返回，不存在时返回空字符串。
func (s *UserSettings) GetForUser(ctx context.Context, userID int64, key string) (string, error) {
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	// value 是用户设置值。
	var value string
	// err 表示读取用户设置值失败。
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id=? AND `+keyColumn+`=?`, userID, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if isSensitiveSettingKey(key) {
		return s.codec.decrypt("user-setting", userSecretOwner(userID), value)
	}
	return value, nil
}

// SetForUser 保存或覆盖用户单项设置；敏感键加密，空敏感值删除。
func (s *UserSettings) SetForUser(ctx context.Context, userID int64, key, value string) error {
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	if isSensitiveSettingKey(key) {
		if strings.TrimSpace(value) == "" {
			// err 表示删除用户设置失败。
			_, err := s.DB.ExecContext(ctx, `DELETE FROM user_settings WHERE user_id=? AND `+keyColumn+`=?`, userID, key)
			return err
		}
		// encrypted 是用户作用域加密后的秘密值。
		encrypted, err := s.codec.encrypt("user-setting", userSecretOwner(userID), value)
		if err != nil {
			return err
		}
		value = encrypted
	}
	// err 是用户设置写入错误。
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO user_settings (user_id, `+keyColumn+`, value, updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)`+
			dialectUpsert(s.Dialect, []string{"user_id", keyColumn}, map[string]string{
				"value": "EXCLUDED.value", "updated_at": "CURRENT_TIMESTAMP",
			}), userID, key, value)
	return err
}

// SetManyForUser 在单个事务内保存多项用户设置，避免只落库一半配置。
// 空映射直接返回 nil：没有待写入的键时不应开启事务。
func (s *UserSettings) SetManyForUser(ctx context.Context, userID int64, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	// tx、err 是本次批量写入的事务与开启错误。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// statement 是单项设置的 upsert 语句，键与值均走占位符。
	statement := `INSERT INTO user_settings (user_id, ` + dialectQuote(s.Dialect, "key") + `, value, updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)` +
		dialectUpsert(s.Dialect, []string{"user_id", dialectQuote(s.Dialect, "key")}, map[string]string{
			"value": "EXCLUDED.value", "updated_at": "CURRENT_TIMESTAMP",
		})
	// key、value 是当前待写入的设置键值对。
	for key, value := range values {
		if isSensitiveSettingKey(key) {
			if strings.TrimSpace(value) == "" {
				// execErr 表示删除旧用户设置失败。
				if _, execErr := tx.ExecContext(ctx, `DELETE FROM user_settings WHERE user_id=? AND `+dialectQuote(s.Dialect, "key")+`=?`, userID, key); execErr != nil {
					return execErr
				}
				continue
			}
			// encrypted 是用户作用域加密后的秘密值。
			encrypted, encryptErr := s.codec.encrypt("user-setting", userSecretOwner(userID), value)
			if encryptErr != nil {
				return encryptErr
			}
			value = encrypted
		}
		// execErr 是当前设置项的写入错误；任一项失败即整体回滚。
		if _, execErr := tx.ExecContext(ctx, statement, userID, key, value); execErr != nil {
			return execErr
		}
	}
	return tx.Commit()
}

// ApplyChangesForUser 原子保存普通用户设置与敏感三态命令。
func (s *UserSettings) ApplyChangesForUser(ctx context.Context, userID int64, values map[string]string, secrets map[string]SensitiveSettingChange) error {
	// tx、err 是本次原子写入的事务与开启错误。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	// updateStatement 是普通设置的 upsert 语句。
	updateStatement := `INSERT INTO user_settings (user_id, ` + keyColumn + `, value, updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)` +
		dialectUpsert(s.Dialect, []string{"user_id", keyColumn}, map[string]string{
			"value": "EXCLUDED.value", "updated_at": "CURRENT_TIMESTAMP",
		})
	// key、value 是当前待写入的普通设置。
	for key, value := range values {
		if isSensitiveSettingKey(key) {
			return errors.New("敏感设置必须通过显式变更命令提交")
		}
		// execErr 表示更新用户设置失败。
		if _, execErr := tx.ExecContext(ctx, updateStatement, userID, key, value); execErr != nil {
			return execErr
		}
	}
	// key、change 是当前待处理的敏感设置键和三态命令。
	for key, change := range secrets {
		switch change.Action {
		case "retain":
			continue
		case "clear":
			// execErr 表示删除敏感用户设置失败。
			if _, execErr := tx.ExecContext(ctx, `DELETE FROM user_settings WHERE user_id=? AND `+keyColumn+`=?`, userID, key); execErr != nil {
				return execErr
			}
		case "replace":
			// encrypted 是用户作用域加密后的新秘密。
			encrypted, encryptErr := s.codec.encrypt("user-setting", userSecretOwner(userID), change.Value)
			if encryptErr != nil {
				return encryptErr
			}
			// execErr 表示更新敏感用户设置失败。
			if _, execErr := tx.ExecContext(ctx, updateStatement, userID, key, encrypted); execErr != nil {
				return execErr
			}
		default:
			return errors.New("敏感设置变更命令无效")
		}
	}
	return tx.Commit()
}

// RedactedForUser 返回用户可见的集成配置视图：非敏感值原样返回，敏感键只返回 configured 标记。
func (s *UserSettings) RedactedForUser(ctx context.Context, userID int64) (map[string]string, error) {
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	// rows、err 是用户设置查询结果集及错误。
	rows, err := s.DB.QueryContext(ctx, `SELECT `+keyColumn+`, value FROM user_settings WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 是脱敏后的设置视图。
	result := make(map[string]string)
	for rows.Next() {
		// key、value 是当前设置行的键和原始值。
		var key, value string
		// err 表示扫描脱敏用户设置行失败。
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if isSensitiveSettingKey(key) {
			result[key+"_configured"] = "true"
			continue
		}
		result[key] = value
	}
	return result, rows.Err()
}
