import type { SystemSettings } from './api';
import { AI_SETTING_KEYS, SETTINGS_SAVE_OMIT_KEYS, SYSTEM_SETTING_KEYS } from './constants';
import type { MCPServiceMessage } from './models';
import type { CredentialsForm,CredentialsMessage,SettingsScope } from './types';

/** 将配置草稿裁剪为当前 scope 允许保存到后端的字段：先剔除兼容字段与空值，再按保存范围白名单过滤。 */
export const buildPersistableSettings = (settings: SystemSettings, scope: SettingsScope): Partial<SystemSettings> => {
  // allowedKeys 是本次保存范围允许写回的字段白名单，防止两页互相覆盖。
  const allowedKeys = scope === 'system' ? SYSTEM_SETTING_KEYS : AI_SETTING_KEYS;
  return Object.fromEntries(
    Object.entries(settings).filter(
      // entry 是配置键值对：仅保留白名单内、且未列入省略键、且非空的字段。
      ([key, value]) => allowedKeys.has(key) && !SETTINGS_SAVE_OMIT_KEYS.has(key) && value !== undefined && value !== null,
    ),
  ) as Partial<SystemSettings>;
};

/** 校验登录凭据表单并返回用户可见错误。 */
export const validateCredentials = (credentials: CredentialsForm): string => {
  // username 是去除首尾空白后的登录用户名。
  const username = credentials.new_username.trim();
  if (username.length < 3) return '用户名至少需要 3 个字符';
  if (!credentials.current_password) return '请输入当前密码确认身份';
  if (credentials.new_password && credentials.new_password.length < 8) return '新密码至少需要 8 个字符';
  if (credentials.new_password !== credentials.confirm_password) return '两次输入的新密码不一致';
  return '';
};

/** 判断设置请求响应是否仍可写入当前页面。 */
export const isCurrentSettingsRequest = (currentSequence: number, requestSequence: number, signal: AbortSignal): boolean => (
  currentSequence === requestSequence && !signal.aborted
);

/** AI 连接测试快照绑定一次出站请求所使用的端点、密钥和模型，不持久化也不展示密钥。 */
export type AIConnectionTestSnapshot = {
  /** baseURL 是实际提交给后端的 AI 服务基础地址。 */
  baseURL: string;
  /** apiKey 是仅在本次请求内转交后端的明文密钥。 */
  apiKey: string;
  /** model 是本次实际验证的模型名称。 */
  model: string;
};

/** isCurrentAIConnectionTest 判断页面当前 AI 配置是否仍与开始测试时的快照一致。 */
export const isCurrentAIConnectionTest = (settings: SystemSettings | null, snapshot: AIConnectionTestSnapshot, defaultBaseURL: string): boolean => {
  // currentBaseURL 保存当前草稿实际用于测试的端点，缺省时与 Hook 使用同一默认地址。
  const currentBaseURL = settings?.ai_api_url || settings?.ai_base_url || defaultBaseURL;
  // currentAPIKey 保存当前草稿的临时密钥；空值表示服务端应读取已保存密钥。
  const currentAPIKey = settings?.ai_api_key || '';
  // currentModel 保存当前草稿的模型名称；空值表示服务端应读取默认模型。
  const currentModel = settings?.ai_model || '';
  return currentBaseURL === snapshot.baseURL && currentAPIKey === snapshot.apiKey && currentModel === snapshot.model;
};

/** 判断错误是否来自主动取消。 */
export const isSettingsAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** 统一提取设置请求错误文本。 */
export const settingsErrorMessage = (error: unknown, fallback: string): string => error instanceof Error ? error.message : fallback;

/** 创建登录凭据的初始表单。 */
export const createCredentials = (username = ''): CredentialsForm => ({
  new_username: username,
  current_password: '',
  new_password: '',
  confirm_password: '',
});

/** 创建凭据保存结果提示。 */
export const createCredentialsMessage = (type: 'success' | 'error', text: string): CredentialsMessage => ({ type, text });

/** 发货内容门禁配置：控制出站发货文本的违禁词拒发与链接健康检查。 */
export type DeliveryGuardConfig = {
  /** enabled 表示发货内容违禁词门禁是否启用，缺省视为启用。 */
  enabled: boolean;
  /** link_check 表示发货前链接健康检查是否启用，缺省视为启用。 */
  link_check: boolean;
  /** extra_block_words 是用户额外违禁词原文，支持逗号或竖线分隔。 */
  extra_block_words: string;
};

/** 发货内容门禁默认配置：两项检查默认启用，且不附加额外违禁词。 */
export const DEFAULT_DELIVERY_GUARD_CONFIG: DeliveryGuardConfig = {
  enabled: true,
  link_check: true,
  extra_block_words: '',
};

/** coerceGuardFlag 归一门禁 JSON 中的布尔开关；缺失或类型不符时回退默认值。 */
const coerceGuardFlag = (value: unknown, fallback: boolean): boolean => {
  if (typeof value === 'boolean') return value;
  if (value === 'true') return true;
  if (value === 'false') return false;
  return fallback;
};

/** 门禁 JSON 解析结果：配置对象与是否需要向用户提示非法 JSON。 */
export type DeliveryGuardParseResult = {
  /** config 是解析或回退后的门禁配置。 */
  config: DeliveryGuardConfig;
  /** invalid 表示原文非法需要提示；缺失或空白不算非法。 */
  invalid: boolean;
};

/** 解析设置草稿中的门禁 JSON；缺失视为默认配置，非法 JSON 回退默认并要求页面提示。 */
export const parseDeliveryGuardConfig = (raw: unknown): DeliveryGuardParseResult => {
  // isBlank 表示服务端尚未写过该键或值为空白文本，此时按默认配置展示且无需告警。
  const isBlank = raw === undefined || raw === null || (typeof raw === 'string' && raw.trim() === '');
  if (isBlank) return { config: { ...DEFAULT_DELIVERY_GUARD_CONFIG }, invalid: false };
  if (typeof raw !== 'string') return { config: { ...DEFAULT_DELIVERY_GUARD_CONFIG }, invalid: true };
  try {
    // parsed 是解析后的门禁 JSON 值，只有普通对象形态才继续读取已知字段。
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { config: { ...DEFAULT_DELIVERY_GUARD_CONFIG }, invalid: true };
    }
    // source 是可安全取字段的门禁 JSON 对象。
    const source = parsed as Record<string, unknown>;
    return {
      config: {
        enabled: coerceGuardFlag(source.enabled, DEFAULT_DELIVERY_GUARD_CONFIG.enabled),
        link_check: coerceGuardFlag(source.link_check, DEFAULT_DELIVERY_GUARD_CONFIG.link_check),
        extra_block_words: typeof source.extra_block_words === 'string' ? source.extra_block_words : DEFAULT_DELIVERY_GUARD_CONFIG.extra_block_words,
      },
      invalid: false,
    };
  } catch {
    return { config: { ...DEFAULT_DELIVERY_GUARD_CONFIG }, invalid: true };
  }
};

/** 将门禁配置序列化为设置草稿存储的 JSON 字符串，供保存系统配置时提交。 */
export const serializeDeliveryGuardConfig = (config: DeliveryGuardConfig): string => JSON.stringify(config);

/** MCP 审计列表的默认页大小。 */
export const MCP_AUDIT_PAGE_SIZE = 10;

/** MCP 审计类别对应的中文标签；未知类别回退原文。 */
export const MCP_AUDIT_CATEGORY_LABELS: Record<string, string> = {
  tool: '工具调用',
  resource: '资源读取',
  prompt: '提示调用',
};

/** 创建 MCP 卡片操作结果提示。 */
export const createMCPServiceMessage = (type: MCPServiceMessage['type'], text: string): MCPServiceMessage => ({ type, text });

/** 把 Unix 秒格式化为本地时间文本；非正数表示从未发生，返回占位符。 */
export const formatMCPUnixSeconds = (value: number): string => {
  if (!Number.isFinite(value) || value <= 0) return '—';
  return new Date(value * 1000).toLocaleString('zh-CN', { hour12: false });
};

/** 构造 Harness 接入本服务的 MCP 配置 JSON 示例；令牌为空时用占位符提示需先生成。 */
export const buildMCPHarnessConfig = (endpoint: string, token: string): string => {
  // authHeader 是示例中展示的 Bearer 头；没有真实令牌时用占位符避免被误当可用凭据。
  const authHeader = `Bearer ${token || '<请先生成令牌>'}`;
  return JSON.stringify({ mcpServers: { 'ydisks-xianyu-helper': { url: endpoint, headers: { Authorization: authHeader } } } }, null, 2);
};
