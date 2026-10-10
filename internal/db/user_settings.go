package db

import (
	"context"
	"database/sql"
)

// UserSettings 保存用户级界面与偏好设置。
type UserSettings struct {
	DB      *sql.DB
	Dialect Dialect
}

// AllForUser 查询指定用户的全部设置。
func (s *UserSettings) AllForUser(ctx context.Context, userID int64) (map[string]string, error) {
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	// rows 和 err 是用户设置查询结果集及错误。
	rows, err := s.DB.QueryContext(ctx, `SELECT `+keyColumn+`, value FROM user_settings WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 是用户设置键值映射。
	result := make(map[string]string)
	for rows.Next() {
		// key 和 value 是当前设置行的键和值。
		var key, value string
		// err 是当前设置行扫描错误。
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, rows.Err()
}

// GetForUser 查询用户单项设置，不存在时返回空字符串。
func (s *UserSettings) GetForUser(ctx context.Context, userID int64, key string) (string, error) {
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	// value 是用户设置值。
	var value string
	// err 是用户单项设置查询错误。
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id=? AND `+keyColumn+`=?`, userID, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetForUser 保存或覆盖用户单项设置。
func (s *UserSettings) SetForUser(ctx context.Context, userID int64, key, value string) error {
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	// err 是用户设置写入错误。
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO user_settings (user_id, `+keyColumn+`, value, updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)`+
			dialectUpsert(s.Dialect, []string{"user_id", keyColumn}, map[string]string{
				"value": "EXCLUDED.value", "updated_at": "CURRENT_TIMESTAMP",
			}), userID, key, value)
	return err
}

// SetManyForUser 在单个事务内保存多项用户设置，避免只落库一半配置。
//
// 空映射直接返回 nil：没有待写入的键时不应开启事务。
func (s *UserSettings) SetManyForUser(ctx context.Context, userID int64, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	// keyColumn 是当前 SQL 方言中安全引用的 key 列名。
	keyColumn := dialectQuote(s.Dialect, "key")
	// tx、err 是本次批量写入的事务与开启错误。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// statement 是单项设置的 upsert 语句，键与值均走占位符。
	statement := `INSERT INTO user_settings (user_id, ` + keyColumn + `, value, updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)` +
		dialectUpsert(s.Dialect, []string{"user_id", keyColumn}, map[string]string{
			"value": "EXCLUDED.value", "updated_at": "CURRENT_TIMESTAMP",
		})
	// key、value 是当前待写入的设置键值对。
	for key, value := range values {
		// execErr 是当前设置项的写入错误；任一项失败即整体回滚。
		if _, execErr := tx.ExecContext(ctx, statement, userID, key, value); execErr != nil {
			return execErr
		}
	}
	return tx.Commit()
}
