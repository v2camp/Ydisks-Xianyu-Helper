package db

import (
	"context"
	"testing"
)

// contextForUserIntegrationTest 返回用户集成测试使用的后台上下文。
func contextForUserIntegrationTest() context.Context {
	return context.Background()
}

// createUserIntegrationOwner 创建并返回一个管理员测试用户主键。
func createUserIntegrationOwner(t *testing.T, store *Store, username string) int64 {
	t.Helper()
	// ctx 是创建测试用户使用的上下文。
	ctx := contextForUserIntegrationTest()
	// created、createErr 是用户创建结果与错误。
	created, createErr := store.Users.Create(ctx, username, username+"@example.com", "pw")
	if createErr != nil || !created {
		t.Fatalf("创建测试用户失败: created=%v err=%v", created, createErr)
	}
	// testUser 是刚创建的测试用户。
	testUser, ownerErr := store.Users.GetByUsername(ctx, username)
	if ownerErr != nil || testUser == nil {
		t.Fatalf("读取测试用户失败: owner=%v err=%v", testUser, ownerErr)
	}
	// adminErr 表示提升测试用户为管理员失败；迁移测试依赖管理员身份。
	if _, adminErr := store.DB.ExecContext(ctx, `UPDATE users SET is_admin=1 WHERE id=?`, testUser.ID); adminErr != nil {
		t.Fatalf("提升测试用户为管理员失败: %v", adminErr)
	}
	return testUser.ID
}

// TestUserSettingsPlainAndSensitiveRoundTrip 验证用户级普通值与敏感值的保存、读取与删除。
func TestUserSettingsPlainAndSensitiveRoundTrip(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// userID 是当前测试用户主键。
	userID := createUserIntegrationOwner(t, store, "user-integration-owner")
	// setErr 表示写入普通用户设置失败。
	if setErr := store.UserSettings.SetForUser(ctx, userID, "smtp_server", "smtp.example.com"); setErr != nil {
		t.Fatal(setErr)
	}
	// secretErr 表示写入敏感用户设置失败。
	if secretErr := store.UserSettings.SetForUser(ctx, userID, "smtp_password", "fixture-secret"); secretErr != nil {
		t.Fatal(secretErr)
	}
	// plain、plainErr 是普通值读取结果与错误。
	plain, plainErr := store.UserSettings.GetForUser(ctx, userID, "smtp_server")
	if plainErr != nil || plain != "smtp.example.com" {
		t.Fatalf("普通值读取异常: value=%q err=%v", plain, plainErr)
	}
	// secret、secretErr 是敏感值解密读取结果与错误。
	secret, secretErr := store.UserSettings.GetForUser(ctx, userID, "smtp_password")
	if secretErr != nil || secret != "fixture-secret" {
		t.Fatalf("敏感值读取异常: value=%q err=%v", secret, secretErr)
	}
	// missing、missingErr 验证不存在的键返回空字符串。
	missing, missingErr := store.UserSettings.GetForUser(ctx, userID, "unknown_key")
	if missingErr != nil || missing != "" {
		t.Fatalf("缺失键读取异常: value=%q err=%v", missing, missingErr)
	}
	// clearErr 表示清空敏感值失败。
	if clearErr := store.UserSettings.SetForUser(ctx, userID, "smtp_password", " "); clearErr != nil {
		t.Fatal(clearErr)
	}
	// cleared、clearedErr 验证敏感值已被删除。
	cleared, clearedErr := store.UserSettings.GetForUser(ctx, userID, "smtp_password")
	if clearedErr != nil || cleared != "" {
		t.Fatalf("敏感值删除异常: value=%q err=%v", cleared, clearedErr)
	}
}

// TestUserSettingsBatchAndChanges 验证批量写入与三态命令的原子保存语义。
func TestUserSettingsBatchAndChanges(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// userID 是当前测试用户主键。
	userID := createUserIntegrationOwner(t, store, "user-integration-owner")
	// emptyErr 验证空映射不开启事务且直接成功。
	if emptyErr := store.UserSettings.SetManyForUser(ctx, userID, nil); emptyErr != nil {
		t.Fatal(emptyErr)
	}
	// batchErr 表示批量写入普通值与敏感值失败。
	batchErr := store.UserSettings.SetManyForUser(ctx, userID, map[string]string{
		"ai_api_url":   "https://ai.example.com/v1",
		"ai_api_key":   "fixture-key",
		"qqbot.app_id": "fixture-app",
	})
	if batchErr != nil {
		t.Fatal(batchErr)
	}
	// changesErr 表示三态命令保存失败。
	changesErr := store.UserSettings.ApplyChangesForUser(ctx, userID,
		map[string]string{"ai_model": "fixture-model"},
		map[string]SensitiveSettingChange{
			"ai_api_key":       {Action: "replace", Value: "rotated-key"},
			"qqbot.app_secret": {Action: "retain"},
			"smtp_password":    {Action: "clear"},
		})
	if changesErr != nil {
		t.Fatal(changesErr)
	}
	// key、keyErr 验证替换后的密钥。
	key, keyErr := store.UserSettings.GetForUser(ctx, userID, "ai_api_key")
	if keyErr != nil || key != "rotated-key" {
		t.Fatalf("密钥替换异常: value=%q err=%v", key, keyErr)
	}
	// model、modelErr 验证普通值写入。
	model, modelErr := store.UserSettings.GetForUser(ctx, userID, "ai_model")
	if modelErr != nil || model != "fixture-model" {
		t.Fatalf("模型写入异常: value=%q err=%v", model, modelErr)
	}
	// sensitivePlainErr 验证普通值通道拒绝敏感键。
	sensitivePlainErr := store.UserSettings.ApplyChangesForUser(ctx, userID, map[string]string{"ai_api_key": "plain"}, nil)
	if sensitivePlainErr == nil {
		t.Fatal("普通值通道应拒绝敏感键")
	}
	// invalidActionErr 验证非法三态命令被拒绝。
	invalidActionErr := store.UserSettings.ApplyChangesForUser(ctx, userID, nil, map[string]SensitiveSettingChange{"ai_api_key": {Action: "unknown"}})
	if invalidActionErr == nil {
		t.Fatal("非法三态命令应被拒绝")
	}
}

// TestUserSettingsRedactedAndAll 验证敏感键只以 configured 标记出现在读取视图。
func TestUserSettingsRedactedAndAll(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// userID 是当前测试用户主键。
	userID := createUserIntegrationOwner(t, store, "user-integration-owner")
	// setErr 表示写入测试配置失败。
	if setErr := store.UserSettings.SetForUser(ctx, userID, "ai_api_key", "fixture-secret"); setErr != nil {
		t.Fatal(setErr)
	}
	// redacted、redactedErr 是脱敏视图与读取错误。
	redacted, redactedErr := store.UserSettings.RedactedForUser(ctx, userID)
	if redactedErr != nil {
		t.Fatal(redactedErr)
	}
	if redacted["ai_api_key_configured"] != "true" || redacted["ai_api_key"] != "" {
		t.Fatalf("脱敏视图泄露敏感值: %+v", redacted)
	}
	// all、allErr 是通用视图与读取错误；敏感键同样只返回标记。
	all, allErr := store.UserSettings.AllForUser(ctx, userID)
	if allErr != nil {
		t.Fatal(allErr)
	}
	if all["ai_api_key_configured"] != "true" || all["ai_api_key"] != "" {
		t.Fatalf("通用视图泄露敏感值: %+v", all)
	}
}

// TestIntegrationSettingKeyHelpers 验证用户集成键白名单与稳定返回。
func TestIntegrationSettingKeyHelpers(t *testing.T) {
	if !IsUserIntegrationSettingKey("ai_api_key") || !IsUserIntegrationSettingKey(" qqbot.app_id ") {
		t.Fatal("合法用户集成键未被识别")
	}
	if IsUserIntegrationSettingKey("theme_color") {
		t.Fatal("实例级键不应归入用户集成配置")
	}
	// keys 是全部用户级集成配置键。
	keys := UserIntegrationSettingKeys()
	if len(keys) != len(userIntegrationSettingKeys) {
		t.Fatalf("用户集成键数量异常: got=%d want=%d", len(keys), len(userIntegrationSettingKeys))
	}
}

// TestReadSensitiveUserSettingAuditsAccess 验证用户级敏感读取先审计且返回解密值。
func TestReadSensitiveUserSettingAuditsAccess(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// userID 是当前测试用户主键。
	userID := createUserIntegrationOwner(t, store, "user-integration-owner")
	// setErr 表示写入敏感用户设置失败。
	if setErr := store.UserSettings.SetForUser(ctx, userID, "ai_api_key", "fixture-secret"); setErr != nil {
		t.Fatal(setErr)
	}
	// value、readErr 是受控读取结果与错误。
	value, readErr := store.ReadSensitiveUserSetting(ctx, userID, "ai_api_key", "settings.use", "test")
	if readErr != nil || value != "fixture-secret" {
		t.Fatalf("受控读取异常: value=%q err=%v", value, readErr)
	}
	// records、listErr 是访问审计记录与查询错误。
	records, listErr := store.SecurityAudit.ListByUser(ctx, userID, 10)
	if listErr != nil || len(records) != 1 || records[0].Keys[0] != "ai_api_key" {
		t.Fatalf("审计记录异常: records=%+v err=%v", records, listErr)
	}
	// invalidUserErr 验证非法用户被拒绝。
	if invalidUserErr := error(nil); invalidUserErr == nil {
		_, invalidUserErr = store.ReadSensitiveUserSetting(ctx, 0, "ai_api_key", "settings.use", "test")
		if invalidUserErr == nil {
			t.Fatal("非法用户应被拒绝")
		}
	}
	// invalidKeyErr 验证非集成键被拒绝。
	if invalidKeyErr := error(nil); invalidKeyErr == nil {
		_, invalidKeyErr = store.ReadSensitiveUserSetting(ctx, userID, "qq_reply_secret_key", "settings.use", "test")
		if invalidKeyErr == nil {
			t.Fatal("非用户集成敏感键应被拒绝")
		}
	}
}

// TestGetUserSettingForAccountResolvesOwner 验证账号读取会解析所有者并分流敏感键。
func TestGetUserSettingForAccountResolvesOwner(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// userID 是当前测试用户主键。
	userID := createUserIntegrationOwner(t, store, "user-integration-owner")
	// saveErr 表示写入测试账号失败。
	if saveErr := store.Cookies.Save(ctx, "owner-account", "unb=1", userID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// setErr 表示写入用户级测试配置失败。
	if setErr := store.UserSettings.SetForUser(ctx, userID, "ai_api_url", "https://ai.example.com/v1"); setErr != nil {
		t.Fatal(setErr)
	}
	// url、urlErr 验证普通设置按所有者读取。
	url, urlErr := store.GetUserSettingForAccount(ctx, "owner-account", "ai_api_url")
	if urlErr != nil || url != "https://ai.example.com/v1" {
		t.Fatalf("账号所有者普通设置读取异常: value=%q err=%v", url, urlErr)
	}
	// secretErr 表示写入用户级密钥失败。
	if secretErr := store.UserSettings.SetForUser(ctx, userID, "ai_api_key", "fixture-secret"); secretErr != nil {
		t.Fatal(secretErr)
	}
	// secret、secretReadErr 验证敏感键按所有者读取并写审计。
	secret, secretReadErr := store.GetUserSettingForAccount(ctx, "owner-account", "ai_api_key")
	if secretReadErr != nil || secret != "fixture-secret" {
		t.Fatalf("账号所有者敏感设置读取异常: value=%q err=%v", secret, secretReadErr)
	}
	// missingErr 验证未知账号返回错误。
	if _, missingErr := store.GetUserSettingForAccount(ctx, "missing-account", "ai_api_url"); missingErr == nil {
		t.Fatal("未知账号应返回错误")
	}
}

// TestMigrateIntegrationSettingsV1FoldsLegacyAssets 验证全局键与渠道级凭据折叠到管理员用户。
func TestMigrateIntegrationSettingsV1FoldsLegacyAssets(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// userID 是当前测试用户主键。
	userID := createUserIntegrationOwner(t, store, "user-integration-owner")
	// seedGlobal 写入旧全局配置键。
	for /* key、value 是当前待写入的旧全局键值。 */ key, value := range map[string]string{
		"ai_api_url": "https://legacy.example.com/v1", "ai_api_key": "legacy-key", "smtp_server": "smtp.legacy.example.com", "qqbot.app_id": "legacy-app",
	} {
		// err 表示写入当前旧全局键失败。
		if err := store.Settings.Set(ctx, key, value); err != nil {
			t.Fatalf("写入旧全局键 %s: %v", key, err)
		}
	}
	// channelID、channelErr 是带渠道级凭据的旧渠道。
	channelID, channelErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "legacy-email", Type: "email", UserID: userID,
		Config: `{"to_email":"legacy@example.com","smtp_user":"legacy-user","smtp_password":"legacy-pass","use_custom_smtp":true}`,
	})
	if channelErr != nil {
		t.Fatal(channelErr)
	}
	// report、migrateErr 是迁移结果与错误。
	report, migrateErr := store.MigrateIntegrationSettingsV1(ctx)
	if migrateErr != nil {
		t.Fatal(migrateErr)
	}
	if report.Skipped || report.AdminUserID != userID || report.GlobalKeys < 4 || report.Channels != 1 {
		t.Fatalf("迁移报告异常: %+v", report)
	}
	// url、urlErr 验证全局 AI 地址折叠到管理员用户。
	url, urlErr := store.UserSettings.GetForUser(ctx, userID, "ai_api_url")
	if urlErr != nil || url != "https://legacy.example.com/v1" {
		t.Fatalf("全局键未折叠: value=%q err=%v", url, urlErr)
	}
	// secret、secretErr 验证全局密钥折叠后仍可解密。
	secret, secretErr := store.UserSettings.GetForUser(ctx, userID, "ai_api_key")
	if secretErr != nil || secret != "legacy-key" {
		t.Fatalf("全局密钥未折叠: value=%q err=%v", secret, secretErr)
	}
	// globalValue、globalErr 验证旧全局键已删除。
	globalValue, globalErr := store.Settings.Get(ctx, "ai_api_url")
	if globalErr != nil || globalValue != "" {
		t.Fatalf("旧全局键未删除: value=%q err=%v", globalValue, globalErr)
	}
	// channel、channelReadErr 验证渠道级凭据已移除。
	channel, channelReadErr := store.Notifications.GetChannel(ctx, channelID)
	if channelReadErr != nil || channel == nil {
		t.Fatal(channelReadErr)
	}
	if containsAny(channel.Config, "legacy-pass", "legacy-user") {
		t.Fatalf("渠道级凭据未清理: %s", channel.Config)
	}
	// again、againErr 验证重复执行会跳过。
	again, againErr := store.MigrateIntegrationSettingsV1(ctx)
	if againErr != nil || !again.Skipped {
		t.Fatalf("重复迁移未跳过: report=%+v err=%v", again, againErr)
	}
}

// TestMigrateIntegrationSettingsV1ConflictAndEmptyPaths 验证冲突保留与无可迁移配置的路径。
func TestMigrateIntegrationSettingsV1ConflictAndEmptyPaths(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// userID 是当前测试用户主键。
	userID := createUserIntegrationOwner(t, store, "user-integration-owner")
	// userSetErr 表示预置用户级配置失败。
	if userSetErr := store.UserSettings.SetForUser(ctx, userID, "ai_api_key", "user-key"); userSetErr != nil {
		t.Fatal(userSetErr)
	}
	// globalSetErr 表示写入冲突旧全局键失败。
	if globalSetErr := store.Settings.Set(ctx, "ai_api_key", "legacy-key"); globalSetErr != nil {
		t.Fatal(globalSetErr)
	}
	// report、migrateErr 是冲突迁移结果与错误。
	report, migrateErr := store.MigrateIntegrationSettingsV1(ctx)
	if migrateErr != nil {
		t.Fatal(migrateErr)
	}
	if report.Conflicts != 1 {
		t.Fatalf("冲突计数异常: %+v", report)
	}
	// kept 验证用户级新值未被覆盖。
	kept, keptErr := store.UserSettings.GetForUser(ctx, userID, "ai_api_key")
	if keptErr != nil || kept != "user-key" {
		t.Fatalf("冲突时用户值被覆盖: value=%q err=%v", kept, keptErr)
	}
	// emptyStore、emptyCleanup 是另一个没有旧配置的数据库。
	emptyStore, emptyCleanup := newTestDB(t)
	defer emptyCleanup()
	// emptyReport、emptyErr 验证全新库直接丢弃默认全局键并标记完成。
	emptyReport, emptyErr := emptyStore.MigrateIntegrationSettingsV1(ctx)
	if emptyErr != nil || !emptyReport.Skipped {
		t.Fatalf("空库迁移异常: report=%+v err=%v", emptyReport, emptyErr)
	}
	// defaultURL、defaultErr 验证出厂默认 AI 地址已删除。
	defaultURL, defaultErr := emptyStore.Settings.Get(ctx, "ai_api_url")
	if defaultErr != nil || defaultURL != "" {
		t.Fatalf("默认全局键未删除: value=%q err=%v", defaultURL, defaultErr)
	}
	// dependencyErr 验证依赖缺失时失败。
	// broken 是缺少依赖的存储。
	broken := &Store{}
	// dependencyErr 表示依赖缺失时的迁移错误。
	if _, dependencyErr := broken.MigrateIntegrationSettingsV1(ctx); dependencyErr == nil {
		t.Fatal("缺少依赖应失败")
	}
}

// TestHasLegacyIntegrationSettingsCoversGlobalAndChannelPaths 验证旧全局键与旧渠道配置的探测分支。
func TestHasLegacyIntegrationSettingsCoversGlobalAndChannelPaths(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// cleanErr 表示清空出厂默认全局键失败。
	if _, cleanErr := store.DB.ExecContext(ctx, `DELETE FROM system_settings`); cleanErr != nil {
		t.Fatal(cleanErr)
	}
	// cleanChannelsErr 表示清空通知渠道失败。
	if _, cleanChannelsErr := store.DB.ExecContext(ctx, `DELETE FROM notification_channels`); cleanChannelsErr != nil {
		t.Fatal(cleanChannelsErr)
	}
	// legacy、legacyErr 验证无旧配置时返回 false。
	legacy, legacyErr := store.hasLegacyIntegrationSettings(ctx)
	if legacyErr != nil || legacy {
		t.Fatalf("空库旧配置探测异常: legacy=%v err=%v", legacy, legacyErr)
	}
	// setErr 表示写入旧全局键失败。
	if setErr := store.Settings.Set(ctx, "ai_api_url", "https://legacy.example.com/v1"); setErr != nil {
		t.Fatal(setErr)
	}
	// globalLegacy、globalErr 验证旧全局键命中。
	globalLegacy, globalErr := store.hasLegacyIntegrationSettings(ctx)
	if globalErr != nil || !globalLegacy {
		t.Fatalf("旧全局键未命中: legacy=%v err=%v", globalLegacy, globalErr)
	}
	// clearErr 表示删除旧全局键失败。
	if _, clearErr := store.DB.ExecContext(ctx, `DELETE FROM system_settings`); clearErr != nil {
		t.Fatal(clearErr)
	}
	// ownerID 是当前测试用户主键，供渠道外键使用。
	ownerID := createUserIntegrationOwner(t, store, "legacy-owner")
	// channelErr 表示写入带配置渠道失败。
	if _, channelErr := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('legacy','webhook','{}',1,`+itoaForTest(ownerID)+`)`); channelErr != nil {
		t.Fatal(channelErr)
	}
	// channelLegacy、channelLegacyErr 验证渠道配置命中。
	channelLegacy, channelLegacyErr := store.hasLegacyIntegrationSettings(ctx)
	if channelLegacyErr != nil || !channelLegacy {
		t.Fatalf("旧渠道配置未命中: legacy=%v err=%v", channelLegacy, channelLegacyErr)
	}
}

// TestFoldChannelCredentialsHandlesFallbackConflictAndDirtyConfig 验证渠道级凭据折叠的边界分支。
func TestFoldChannelCredentialsHandlesFallbackConflictAndDirtyConfig(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// adminID 是接收渠道凭据的管理员用户主键。
	adminID := createUserIntegrationOwner(t, store, "fold-admin")
	// conflictErr 表示预置冲突用户级值失败。
	if conflictErr := store.UserSettings.SetForUser(ctx, adminID, "qqbot.app_id", "user-app"); conflictErr != nil {
		t.Fatal(conflictErr)
	}
	// fixtures 保存覆盖冲突、空值与损坏配置的渠道夹具。
	fixtures := []string{
		`INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('conflict','qq','{"app_id":"channel-app"}',1,` + itoaForTest(adminID) + `)`,
		`INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('blank','qq','{"app_id":"  "}',1,` + itoaForTest(adminID) + `)`,
		`INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('broken','qq','not-json',1,` + itoaForTest(adminID) + `)`,
		`INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES ('empty','qq','',1,` + itoaForTest(adminID) + `)`,
	}
	// fixture 是当前待写入的渠道夹具。
	for _, fixture := range fixtures {
		// execErr 表示写入当前渠道夹具失败。
		if _, execErr := store.DB.ExecContext(ctx, fixture); execErr != nil {
			t.Fatal(execErr)
		}
	}
	// folded、conflicts、foldErr 是折叠结果与冲突计数。
	folded, conflicts, foldErr := store.foldChannelCredentials(ctx, adminID)
	if foldErr != nil {
		t.Fatal(foldErr)
	}
	if folded != 1 || conflicts != 1 {
		t.Fatalf("折叠结果异常: folded=%d conflicts=%d", folded, conflicts)
	}
	// kept 验证冲突时保留用户级值。
	kept, keptErr := store.UserSettings.GetForUser(ctx, adminID, "qqbot.app_id")
	if keptErr != nil || kept != "user-app" {
		t.Fatalf("冲突值覆盖异常: value=%q err=%v", kept, keptErr)
	}
}

// itoaForTest 是无额外依赖的整数转字符串测试辅助。
func itoaForTest(value int64) string {
	// digits 保存十进制数字字符。
	digits := ""
	if value == 0 {
		return "0"
	}
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// TestReadSensitiveUserSettingRejectsInvalidContext 验证受控读取的依赖、身份与审计拒绝分支。
func TestReadSensitiveUserSettingRejectsInvalidContext(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// ownerID 是测试用户主键。
	ownerID := createUserIntegrationOwner(t, store, "sensitive-owner")
	// broken 是缺少用户设置仓储的存储。
	broken := &Store{}
	// brokenErr 表示缺少用户设置仓储时的读取错误。
	if _, brokenErr := broken.ReadSensitiveUserSetting(ctx, ownerID, "ai_api_key", "settings.use", "test"); brokenErr == nil {
		t.Fatal("缺少用户设置仓储应失败")
	}
	// missingAudit 临时保存原安全审计依赖，便于模拟缺失审计。
	missingAudit := store.SecurityAudit
	store.SecurityAudit = nil
	// auditErr 表示缺少安全审计时的读取错误。
	_, auditErr := store.ReadSensitiveUserSetting(ctx, ownerID, "ai_api_key", "settings.use", "test")
	store.SecurityAudit = missingAudit
	if auditErr == nil {
		t.Fatal("缺少安全审计应失败")
	}
	// invalidActionErr 验证空审计上下文被拒绝。
	if _, invalidActionErr := store.ReadSensitiveUserSetting(ctx, ownerID, "ai_api_key", "", "test"); invalidActionErr == nil {
		t.Fatal("空审计动作应失败")
	}
}

// TestGetUserSettingForAccountRejectsMissingDependencies 验证账号读取的依赖缺失分支。
func TestGetUserSettingForAccountRejectsMissingDependencies(t *testing.T) {
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// broken 是缺少依赖的存储。
	broken := &Store{}
	// brokenErr 表示缺少依赖时的账号读取错误。
	if _, brokenErr := broken.GetUserSettingForAccount(ctx, "cid", "ai_api_url"); brokenErr == nil {
		t.Fatal("缺少依赖应失败")
	}
}

// TestSetManyForUserDeletesEmptySensitiveValue 验证批量写入用空敏感值删除旧秘密。
func TestSetManyForUserDeletesEmptySensitiveValue(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// ownerID 是测试用户主键。
	ownerID := createUserIntegrationOwner(t, store, "batch-owner")
	// seedErr 表示写入旧秘密失败。
	if seedErr := store.UserSettings.SetForUser(ctx, ownerID, "ai_api_key", "old-key"); seedErr != nil {
		t.Fatal(seedErr)
	}
	// batchErr 表示批量写入空敏感值失败。
	if batchErr := store.UserSettings.SetManyForUser(ctx, ownerID, map[string]string{"ai_api_key": " "}); batchErr != nil {
		t.Fatal(batchErr)
	}
	// value、valueErr 验证旧秘密已删除。
	value, valueErr := store.UserSettings.GetForUser(ctx, ownerID, "ai_api_key")
	if valueErr != nil || value != "" {
		t.Fatalf("空敏感值未删除: value=%q err=%v", value, valueErr)
	}
}

// TestUserSettingsErrorBranches 验证数据库关闭后的错误返回路径。
func TestUserSettingsErrorBranches(t *testing.T) {
	// store、cleanup 是隔离的用户设置测试仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本次测试使用的上下文。
	ctx := contextForUserIntegrationTest()
	// closeErr 表示关闭测试数据库失败。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// 数据库关闭后所有读取与写入都应返回错误。
	// allErr 表示关闭数据库后的通用读取错误。
	if _, allErr := store.UserSettings.AllForUser(ctx, 1); allErr == nil {
		t.Fatal("AllForUser 应返回错误")
	}
	// getErr 表示关闭数据库后的单项读取错误。
	if _, getErr := store.UserSettings.GetForUser(ctx, 1, "ai_api_url"); getErr == nil {
		t.Fatal("GetForUser 应返回错误")
	}
	// setErr 表示关闭数据库后的单项写入错误。
	if setErr := store.UserSettings.SetForUser(ctx, 1, "ai_api_url", "x"); setErr == nil {
		t.Fatal("SetForUser 应返回错误")
	}
	// setManyErr 表示关闭数据库后的批量写入错误。
	if setManyErr := store.UserSettings.SetManyForUser(ctx, 1, map[string]string{"ai_api_url": "x"}); setManyErr == nil {
		t.Fatal("SetManyForUser 应返回错误")
	}
	// changesErr 表示关闭数据库后的三态保存错误。
	if changesErr := store.UserSettings.ApplyChangesForUser(ctx, 1, map[string]string{"ai_api_url": "x"}, nil); changesErr == nil {
		t.Fatal("ApplyChangesForUser 应返回错误")
	}
	// redactedErr 表示关闭数据库后的脱敏读取错误。
	if _, redactedErr := store.UserSettings.RedactedForUser(ctx, 1); redactedErr == nil {
		t.Fatal("RedactedForUser 应返回错误")
	}
	// sensitiveSetErr 表示关闭数据库后的敏感写入错误。
	if sensitiveSetErr := store.UserSettings.SetForUser(ctx, 1, "ai_api_key", "x"); sensitiveSetErr == nil {
		t.Fatal("敏感 SetForUser 应返回错误")
	}
	// deleteLegacyErr 表示关闭数据库后的默认键删除错误。
	if deleteLegacyErr := store.deleteLegacyGlobalKeys(ctx); deleteLegacyErr == nil {
		t.Fatal("deleteLegacyGlobalKeys 应返回错误")
	}
	// adminLookupErr 表示关闭数据库后的管理员查询错误。
	if _, adminLookupErr := store.integrationMigrationAdminID(ctx); adminLookupErr == nil {
		t.Fatal("integrationMigrationAdminID 应返回错误")
	}
	// legacyProbeErr 表示关闭数据库后的旧配置探测错误。
	if _, legacyProbeErr := store.hasLegacyIntegrationSettings(ctx); legacyProbeErr == nil {
		t.Fatal("hasLegacyIntegrationSettings 应返回错误")
	}
	// foldErr 表示关闭数据库后的渠道折叠错误。
	if _, _, foldErr := store.foldChannelCredentials(ctx, 1); foldErr == nil {
		t.Fatal("foldChannelCredentials 应返回错误")
	}
	// migrateErr 表示关闭数据库后的迁移错误。
	if _, migrateErr := store.MigrateIntegrationSettingsV1(ctx); migrateErr == nil {
		t.Fatal("MigrateIntegrationSettingsV1 应返回错误")
	}
}
