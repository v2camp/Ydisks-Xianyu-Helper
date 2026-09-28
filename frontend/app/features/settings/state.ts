import type { SystemSettings } from './api';
import { AI_SETTING_KEYS, SETTINGS_SAVE_OMIT_KEYS, SYSTEM_SETTING_KEYS } from './constants';
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
