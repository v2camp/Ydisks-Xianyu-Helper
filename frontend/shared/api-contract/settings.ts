// settings 只公开 OpenAPI 生成的设置传输类型和跨 feature 共用的更新契约；表单状态属于各自 feature。
import type { components } from './generated/schema';

/** SystemSettingsTransport 表示受约束动态键的设置响应。 */
export type SystemSettingsTransport = components['schemas']['SystemSettingsResponse'];
/** AIModelsTransport 表示生成的模型发现响应。 */
export type AIModelsTransport = components['schemas']['AIModelsResponse'];

/** 敏感系统设置的显式三态变更命令。 */
export type SensitiveSettingChange = {
  /** 敏感值的保存策略。 */
  action: 'retain' | 'replace' | 'clear';
  /** replace 策略需要保存的新值，只存在于提交请求中。 */
  value?: string;
};

/** 系统设置更新请求，将普通配置与敏感命令分开传输。 */
export type SystemSettingsUpdate = {
  /** 非敏感配置字段。 */
  values?: Record<string, string | number | boolean>;
  /** 由服务端解释的敏感值变更命令。 */
  secrets?: Record<string, SensitiveSettingChange>;
};

/** SENSITIVE_SYSTEM_SETTING_KEYS 标识不得写入普通 values 的秘密键。 */
export const SENSITIVE_SYSTEM_SETTING_KEYS = new Set([
  'ai_api_key',
  'smtp_password',
  'qq_reply_secret_key',
  'captcha.remote_secret_key',
  'qqbot.app_secret',
]);

// QQ 连接器就绪判定使用的设置键：连接器注册表仍是连接器页的 UI 权威，这里只公开跨 feature 的只读判定。
export const QQ_CONNECTOR_READY_KEYS = {
  /** appId 是 QQ 机器人 AppID 的设置键。 */
  appId: 'qqbot.app_id',
  /** appSecretConfigured 是 AppSecret 是否已配置的状态键，服务端只回传布尔状态而不回传明文。 */
  appSecretConfigured: 'qqbot.app_secret_configured',
} as const;

/** isQQConnectorReady 判断 QQ 机器人凭据是否足以启用运营 Agent：AppID 去空白后非空，且 AppSecret 已配置。 */
export const isQQConnectorReady = (settings: Record<string, unknown>): boolean => {
  // appId 是去除首尾空白后的 AppID；非字符串或纯空白都等同于未配置。
  const appId = typeof settings[QQ_CONNECTOR_READY_KEYS.appId] === 'string' ? String(settings[QQ_CONNECTOR_READY_KEYS.appId]).trim() : '';
  // secretConfigured 只认布尔 true；字符串 'true' 已由设置适配层在归一阶段转成布尔值。
  const secretConfigured = settings[QQ_CONNECTOR_READY_KEYS.appSecretConfigured] === true;
  return appId !== '' && secretConfigured;
};

/** 将各 feature 的设置草稿转换为服务端要求的普通值与敏感命令结构。 */
export const normalizeSystemSettingsUpdate = (settings: Record<string, string | number | boolean | undefined> | SystemSettingsUpdate): SystemSettingsUpdate => {
  if ('values' in settings || 'secrets' in settings) return settings as SystemSettingsUpdate;
  // values 保存普通设置，永不容纳秘密文本。
  const values: Record<string, string | number | boolean> = {};
  // secrets 保存服务端需要独立处理的敏感设置命令。
  const secrets: Record<string, SensitiveSettingChange> = {};
  for (const /* key、value 是当前待分流的设置键和值。 */ [key, value] of Object.entries(settings)) {
    if (value === undefined || value === null) continue;
    if (SENSITIVE_SYSTEM_SETTING_KEYS.has(key)) {
      secrets[key] = value === '' ? { action: 'clear' } : { action: 'replace', value: String(value) };
      continue;
    }
    if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') values[key] = value;
  }
  return Object.keys(secrets).length > 0 ? { values, secrets } : values;
};
