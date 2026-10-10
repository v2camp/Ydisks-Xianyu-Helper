import type { AgentPresetOption,AgentSupportSettings,AgentSupportSettingsUpdate,MCPAuditPage,MCPAuditRecord,MCPServiceStatus,OperationResponse,SystemSettings } from './models';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { normalizeSystemSettingsUpdate,SENSITIVE_SYSTEM_SETTING_KEYS } from '../../../shared/api-contract/settings';
import { type RequestControlOptions } from '../../../shared/http/client';
import { collectionFrom } from '../../../shared/http/contract';
import type { SystemSettingsUpdate } from '../../../shared/api-contract/settings';
export type * from './models';
export { normalizeSystemSettingsUpdate } from '../../../shared/api-contract/settings';
export type { SensitiveSettingChange,SystemSettingsUpdate } from '../../../shared/api-contract/settings';

/** 设置页面使用的会话状态传输契约，避免跨 feature 依赖。 */
export interface SettingsSessionStatusResponse {
  /** 当前浏览器会话是否已认证。 */
  authenticated: boolean;
  /** 服务是否已经完成首次管理员初始化。 */
  initialized?: boolean;
  /** 当前用户的数据库标识。 */
  user_id?: number;
  /** 当前用户的登录名称。 */
  username?: string;
  /** 当前用户是否具有管理员权限。 */
  is_admin?: boolean;
}

/** 将服务端可兼容的字符串和数值设置归一为 UI 可消费的只读契约。 */
const normalizeSettings = (settings: Record<string, unknown>): SystemSettings => {
  // result 是不修改原始响应的设置副本。
  const result: Record<string, unknown> = { ...settings };
  // sensitiveKey 是当前需要转为布尔状态的敏感键。
  for (const /* sensitiveKey 是当前需要读取配置状态的敏感设置键。 */ sensitiveKey of SENSITIVE_SYSTEM_SETTING_KEYS) {
    // configuredKey 是敏感键对应的配置状态字段。
    const configuredKey = `${sensitiveKey}_configured`;
    if (configuredKey in result) result[configuredKey] = result[configuredKey] === true || result[configuredKey] === 'true';
  }
  if ('renewal_log_retention_days' in result) {
    // days 是完成数值转换后的日志保留天数。
    const days = Number(result.renewal_log_retention_days);
    result.renewal_log_retention_days = Number.isFinite(days) ? days : 10;
  }
  if ('outbound_http_public_only' in result) {
    // publicOnly 保存统一出站策略的布尔状态，兼容旧服务端返回的字符串。
    result.outbound_http_public_only = result.outbound_http_public_only === true || result.outbound_http_public_only === 'true';
  }
  if ('ai_reply_review_mode' in result) {
    // reviewEnabled 是人工确认开关的布尔状态，匹配引擎接受的真值集合。
    const raw = result.ai_reply_review_mode;
    result.ai_reply_review_mode = typeof raw === 'string' && ['1', 'true', 'yes', 'on', 'enabled'].includes(raw.toLowerCase());
  }
  if ('global_send_daily_limit' in result) {
    // limit 是完成数值转换后的全局日发送额度，非有限值回落不限（0）。
    const limit = Number(result.global_send_daily_limit);
    result.global_send_daily_limit = Number.isFinite(limit) ? limit : 0;
  }
  if ('silence_alert_minutes' in result) {
    // minutes 是完成数值转换后的静默告警阈值，非有限值回落默认 180。
    const minutes = Number(result.silence_alert_minutes);
    result.silence_alert_minutes = Number.isFinite(minutes) ? minutes : 180;
  }
  return result as SystemSettings;
};

/** 修改当前管理员的密码。 */
export const changePassword = async (currentPassword: string, newPassword: string): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次管理员密码更新请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/session/password', { body: { current_password: currentPassword, new_password: newPassword }, signal }));

/** 修改当前管理员的登录名称与可选密码。 */
export const updateLoginCredentials = async (data: { /** 当前密码用于服务端重新验证身份。 */ current_password: string; /** 新的管理员名称。 */ new_username: string; /** 可选的新密码。 */ new_password?: string }, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次管理员凭据更新请求的超时与取消控制信号。 */ signal => contractClient.PUT('/api/v1/session/credentials', { body: data, signal }), options);

/** 获取系统设置；仅保留敏感值是否已配置的状态，不接收敏感明文。 */
export const getSystemSettings = async (options?: RequestControlOptions): Promise<SystemSettings> => {
  // response 是 OpenAPI 约束的脱敏系统设置键值对象。
  const response = await runContractRequest(/* signal 是本次系统设置读取请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/settings/system', { signal }), options);
  return normalizeSettings(response);
};

/** 保存普通系统配置和敏感设置命令。 */
export const updateSystemSettings = async (settings: Partial<SystemSettings> | SystemSettingsUpdate, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 是本次系统设置更新请求的超时与取消控制信号。 */ signal => contractClient.PUT('/api/v1/settings/system', {
    body: normalizeSystemSettingsUpdate(settings),
    signal,
  }), options);

/** 向服务端请求指定人工智能服务的可用模型列表。 */
export const fetchAIModels = async (baseURL: string, apiKey = '', options?: RequestControlOptions): Promise<string[]> => {
  // response 是模型发现接口的具名响应。
  const response = await runContractRequest(/* signal 是本次模型发现请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/settings/ai-models', {
    body: { base_url: baseURL, api_key: apiKey },
    signal,
  }), options);
  return Array.isArray(response.models) ? response.models : [];
};

/** AI 连接测试结果。 */
export interface AIConnectionTestResult {
  /** 测试是否成功。 */
  success: boolean;
  /** 实际被测试的模型名称。 */
  model: string;
  /** 请求耗时毫秒数。 */
  latency_ms: number;
  /** 模型回复摘要。 */
  reply: string;
}

/** 发送一次最小对话请求验证 AI API 地址、密钥和模型的组合是否可用。 */
export const testAIConnection = async (baseURL: string, apiKey: string, model: string, options?: RequestControlOptions): Promise<AIConnectionTestResult> => {
  // response 是由版本化 OpenAPI operation 校验后的非敏感连接诊断结果。
  const response = await runContractRequest(/* signal 是本次长时连接测试的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/settings/ai-test', {
    body: { base_url: baseURL, api_key: apiKey, model },
    signal,
  }), { timeoutMs: 60_000, ...options });
  return response;
};

/** 在保存登录凭据前读取当前会话状态。 */
export const verifySession = async (options?: RequestControlOptions): Promise<SettingsSessionStatusResponse> =>
  runContractRequest(/* signal 是本次设置页会话校验请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/session', { signal }), options);

/** MCP 审计查询参数；页码从 1 开始，未提供的过滤项不参与筛选。 */
export interface MCPAuditQuery {
  /** 从 1 开始的页码。 */
  page?: number;
  /** 每页条数，最大 200。 */
  pageSize?: number;
  /** 调用类别过滤：tool、resource 或 prompt。 */
  category?: 'tool' | 'resource' | 'prompt';
  /** 工具/资源/提示名称精确过滤。 */
  name?: string;
  /** 目标账号标识过滤。 */
  accountId?: string;
  /** 调用成败过滤；不传表示全部。 */
  success?: boolean;
}

/** MCP 启用开关与网络策略更新请求。 */
export interface MCPSettingsUpdate {
  /** 更新后的启用状态。 */
  enabled: boolean;
  /** 更新后的非本机访问策略。 */
  allowNonLoopback: boolean;
}

/** 读取 MCP 对外服务状态；结果只含非敏感配置态。 */
export const getMCPServiceStatus = async (options?: RequestControlOptions): Promise<MCPServiceStatus> => {
  // response 是 OpenAPI 约束的 MCP 状态响应。
  const response = await runContractRequest(/* signal 控制 MCP 状态读取的取消和超时。 */ signal => contractClient.GET('/api/v1/mcp/status', { signal }), options);
  return {
    enabled: response.enabled === true,
    allowNonLoopback: response.allow_non_loopback === true,
    hasToken: response.has_token === true,
    tokenCreatedAt: Number(response.token_created_at) || 0,
    tokenLastUsedAt: Number(response.token_last_used_at) || 0,
    hasPreviousToken: response.has_previous_token === true,
    previousTokenExpiresAt: Number(response.previous_token_expires_at) || 0,
    endpoint: typeof response.endpoint === 'string' ? response.endpoint : '',
  };
};

/** 保存 MCP 启用开关与网络策略。 */
export const updateMCPServiceSettings = async (update: MCPSettingsUpdate, options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 控制 MCP 设置保存的取消和超时。 */ signal => contractClient.PUT('/api/v1/mcp/settings', {
    body: { enabled: update.enabled, allow_non_loopback: update.allowNonLoopback },
    signal,
  }), options);

/** 生成或轮换 MCP 持久化令牌，返回只在本次响应出现一次的明文。 */
export const generateMCPToken = async (options?: RequestControlOptions): Promise<string> => {
  // response 是携带一次性令牌明文的响应。
  const response = await runContractRequest(/* signal 控制 MCP 令牌生成的取消和超时。 */ signal => contractClient.POST('/api/v1/mcp/token', { signal }), options);
  return typeof response.token === 'string' ? response.token : '';
};

/** 吊销全部 MCP 持久化令牌。 */
export const revokeMCPToken = async (options?: RequestControlOptions): Promise<OperationResponse> =>
  runContractRequest(/* signal 控制 MCP 令牌吊销的取消和超时。 */ signal => contractClient.DELETE('/api/v1/mcp/token', { signal }), options);

/** 把单条审计 DTO 归一为 UI 模型，避免页面直接消费生成契约的字段。 */
const toMCPAuditRecord = (row: {
  /** 审计行主键。 */
  id?: number;
  /** 调用发生时间的 Unix 秒。 */
  created_at?: number;
  /** 执行调用的管理员用户标识。 */
  user_id?: number;
  /** 令牌来源标识。 */
  token_source?: string;
  /** 调用类别。 */
  category?: string;
  /** 被调用的名称。 */
  name?: string;
  /** 目标账号标识。 */
  cookie_id?: string;
  /** 脱敏后的参数摘要。 */
  arguments?: string;
  /** 调用是否成功。 */
  success?: boolean;
  /** 失败类别标识。 */
  error_class?: string;
  /** 调用耗时毫秒。 */
  duration_ms?: number;
}): MCPAuditRecord => ({
  id: Number(row.id) || 0,
  createdAt: Number(row.created_at) || 0,
  userId: Number(row.user_id) || 0,
  tokenSource: typeof row.token_source === 'string' ? row.token_source : '',
  category: typeof row.category === 'string' ? row.category : '',
  name: typeof row.name === 'string' ? row.name : '',
  cookieId: typeof row.cookie_id === 'string' ? row.cookie_id : '',
  arguments: typeof row.arguments === 'string' ? row.arguments : '',
  success: row.success === true,
  errorClass: typeof row.error_class === 'string' ? row.error_class : '',
  durationMs: Number(row.duration_ms) || 0,
});

/** 分页查询 MCP 调用审计；参数摘要已由服务端脱敏。 */
export const getMCPAudit = async (query: MCPAuditQuery = {}, options?: RequestControlOptions): Promise<MCPAuditPage> => {
  // response 是 OpenAPI 约束的审计分页响应。
  const response = await runContractRequest(/* signal 控制 MCP 审计查询的取消和超时。 */ signal => contractClient.GET('/api/v1/mcp/audit', {
    params: {
      query: {
        page: query.page,
        page_size: query.pageSize,
        category: query.category,
        name: query.name,
        account_id: query.accountId,
        success: query.success,
      },
    },
    signal,
  }), options);
  // records 是归一后的审计记录列表；缺失时回落空数组。
  const records = Array.isArray(response.data) ? response.data.map(toMCPAuditRecord) : [];
  return {
    records,
    total: Number(response.total) || 0,
    page: Number(response.page) || 1,
    pageSize: Number(response.page_size) || records.length,
    totalPages: Number(response.total_pages) || 0,
  };
};

// parseAgentPresetOptions 把服务端下发的档位名单归一为 UI 模型；缺少稳定标识的条目无法回传，直接丢弃。
const parseAgentPresetOptions = (payload: unknown): AgentPresetOption[] => collectionFrom<Record<string, unknown>>(payload, ['presets'])
  .filter(/* entry 是当前待校验的档位条目，value 缺失或为空串的条目一律不作候选。 */ entry => typeof entry?.value === 'string' && entry.value !== '')
  .map(/* entry 是当前档位条目，逐字段归一为可供界面渲染的选项模型。 */ entry => ({
    value: String(entry.value),
    label: String(entry.label ?? ''),
    description: String(entry.description ?? ''),
  }));

/** 读取租户级客服 Agent 的默认开关、默认档位与服务端下发的可选档位名单。 */
export const getAgentSupportSettings = async (options?: RequestControlOptions): Promise<AgentSupportSettings> => {
  // response 是 OpenAPI 约束的客服 Agent 租户配置响应。
  const response = await runContractRequest(/* signal 控制客服 Agent 租户配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/agent/support/settings', { signal }), options);
  return {
    enabled: response.enabled === true,
    preset: typeof response.preset === 'string' ? response.preset : '',
    presets: parseAgentPresetOptions(response.presets),
  };
};

/** 保存租户级客服 Agent 的默认开关与档位；该配置是未单独覆盖的账号的继承来源。 */
export const updateAgentSupportSettings = async (update: AgentSupportSettingsUpdate, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制客服 Agent 租户配置保存的取消和超时。 */ signal => contractClient.PUT('/api/v1/agent/support/settings', {
    body: { enabled: update.enabled, preset: update.preset },
    signal,
  }), options);
};
