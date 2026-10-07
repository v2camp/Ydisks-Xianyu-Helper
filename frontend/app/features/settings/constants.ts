/** AI 服务默认兼容 API 地址。 */
export const DEFAULT_AI_API_URL = 'https://dashscope.aliyuncs.com/compatible-mode/v1';

/** 日志等级选择项。 */
export const LOG_LEVELS = [
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warn' },
  { value: 'error', label: 'Error' },
];

/** 不应通过系统配置批量保存的兼容字段集合。 */
export const SETTINGS_SAVE_OMIT_KEYS = new Set([
	'ai_api_key_configured',
	'smtp_password_configured',
	'qq_reply_secret_key_configured',
  'captcha.remote_secret_key_configured',
  'qqbot.app_secret_configured',
  'smtp_server',
  'smtp_port',
  'smtp_user',
  'smtp_password',
  'smtp_from',
  'smtp_from_name',
  'smtp_from_address',
  'registration_enabled',
  'show_default_login_info',
  'login_captcha_enabled',
  'item_sync_enabled',
  'item_sync_interval',
  'item_sync_max_pages',
  'default_reply',
]);

/** 系统设置页允许保存的字段白名单；命中后仍会经过省略键与空值过滤。 */
export const SYSTEM_SETTING_KEYS = new Set([
  'log_level',
  'log_format',
  'renewal_log_retention_days',
  'outbound_http_public_only',
  'global_send_daily_limit',
  'silence_alert_minutes',
  'qqbot.app_id',
  'qqbot.app_secret',
  'qqbot.commands_enabled',
  'qqbot.command_openids',
  'captcha.remote_service_url',
  'captcha.remote_secret_key',
  'captcha.remote_pass_cookies',
  'delivery_content_guard',
]);

/** AI 设置页允许保存的字段白名单；命中后仍会经过省略键与空值过滤。 */
export const AI_SETTING_KEYS = new Set([
  'ai_api_url',
  'ai_api_key',
  'ai_model',
  'ai_reply_review_mode',
  'ai_scope_config',
  'ai_knowledge_config',
  'ai_policy_config',
  'mcp.servers',
]);
