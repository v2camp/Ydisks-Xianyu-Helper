import type { AgentConversationMessage,AgentConversationMessagePage,AgentPresetOption,AgentSupportSettings,AgentSupportSettingsUpdate,AgentTakeoverConversationPage,AgentTakeoverSession,MCPAuditPage,MCPAuditRecord,MCPServiceStatus,OperationResponse,OpsConversation,OpsConversationPage,OpsMessage,OpsMessagePage,OpsSendMessageResult,SystemSettings } from './models';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { isQQConnectorReady,normalizeSystemSettingsUpdate,SENSITIVE_SYSTEM_SETTING_KEYS } from '../../../shared/api-contract/settings';
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
  // 总闸缺省语义是开启：只有显式写入 "false" 才算关闭，因此未配置时必须回落 true。
  // masterRaw 是服务端返回的总闸原始值，可能是布尔、数字或字符串。
  const masterRaw = result.ai_master_enabled;
  result.ai_master_enabled = masterRaw === undefined || masterRaw === null
    ? true
    : masterRaw === true || masterRaw === 1 || masterRaw === '1' || String(masterRaw).toLowerCase() === 'true';
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

/** 读取租户级客服 Agent 的默认档位与服务端下发的可选档位名单；是否运行由 AI 生效值统一决定。 */
export const getAgentSupportSettings = async (options?: RequestControlOptions): Promise<AgentSupportSettings> => {
  // response 是 OpenAPI 约束的客服 Agent 租户配置响应，只含档位相关字段。
  const response = await runContractRequest(/* signal 控制客服 Agent 租户配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/agent/support/settings', { signal }), options);
  return {
    preset: typeof response.preset === 'string' ? response.preset : '',
    presets: parseAgentPresetOptions(response.presets),
  };
};

/** 保存租户级客服 Agent 的默认档位；该配置是未单独覆盖的账号的继承来源。 */
export const updateAgentSupportSettings = async (update: AgentSupportSettingsUpdate, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制客服 Agent 租户配置保存的取消和超时。 */ signal => contractClient.PUT('/api/v1/agent/support/settings', {
    body: { preset: update.preset },
    signal,
  }), options);
};

/** 判断 QQ 机器人凭据是否足以启用运营 Agent；Hook 只能经本 feature adapter 使用该判定，不得直接依赖 transport 模块。 */
export const isQQBotConnectorReady = isQQConnectorReady;

/** 把客服接管会话 DTO 归一为 UI 模型；缺失字段按空串或 0 回落。 */
const toAgentTakeoverSession = (row: {
  /** 账号标识。 */
  account_id?: string;
  /** 会话标识。 */
  chat_id?: string;
  /** 对端平台标识。 */
  peer_user_id?: string;
  /** 对端展示名称。 */
  peer_name?: string;
  /** 对端头像地址。 */
  peer_avatar_url?: string;
  /** 当前账号角色。 */
  account_role?: string;
  /** 买家平台标识。 */
  buyer_user_id?: string;
  /** 卖家平台标识。 */
  seller_user_id?: string;
  /** 角色结论绑定的商品标识。 */
  role_item_id?: string;
  /** 角色证据来源。 */
  role_source?: string;
  /** 会话商品标识。 */
  item_id?: string;
  /** 会话商品标题。 */
  item_title?: string;
  /** 会话商品主图地址。 */
  item_image_url?: string;
  /** 最近消息摘要。 */
  last_message?: string;
  /** 最近消息毫秒时间。 */
  last_message_at?: number;
  /** 未读数量。 */
  unread_count?: number;
  /** AI 生成轮次近似计数。 */
  takeover_count?: number;
}): AgentTakeoverSession => ({
  accountId: String(row.account_id ?? ''),
  chatId: String(row.chat_id ?? ''),
  peerUserId: String(row.peer_user_id ?? ''),
  peerName: String(row.peer_name ?? ''),
  peerAvatarUrl: String(row.peer_avatar_url ?? ''),
  accountRole: String(row.account_role ?? ''),
  buyerUserId: String(row.buyer_user_id ?? ''),
  sellerUserId: String(row.seller_user_id ?? ''),
  roleItemId: String(row.role_item_id ?? ''),
  roleSource: String(row.role_source ?? ''),
  itemId: String(row.item_id ?? ''),
  itemTitle: String(row.item_title ?? ''),
  itemImageUrl: String(row.item_image_url ?? ''),
  lastMessage: String(row.last_message ?? ''),
  lastMessageAt: Number(row.last_message_at) || 0,
  unreadCount: Number(row.unread_count) || 0,
  takeoverCount: Number(row.takeover_count) || 0,
});

/** 把聊天消息 DTO 归一为接管历史 UI 模型；字段名从 snake_case 转到 camelCase。 */
const toAgentConversationMessage = (row: {
  /** 本地消息主键。 */
  id?: number;
  /** 账号标识。 */
  account_id?: string;
  /** 会话标识。 */
  chat_id?: string;
  /** 消息幂等键。 */
  message_key?: string;
  /** 消息方向。 */
  direction?: string;
  /** 发送者标识。 */
  sender_id?: string;
  /** 发送者名称。 */
  sender_name?: string;
  /** 消息类型。 */
  message_type?: string;
  /** 文本或媒体地址。 */
  content?: string;
  /** 语音时长。 */
  media_duration?: number;
  /** 投递状态。 */
  status?: string;
  /** 已读状态。 */
  read_status?: number;
  /** 已读时间毫秒。 */
  read_at?: number;
  /** 发送时间毫秒。 */
  sent_at?: number;
}): AgentConversationMessage => ({
  id: Number(row.id) || 0,
  accountId: String(row.account_id ?? ''),
  chatId: String(row.chat_id ?? ''),
  messageKey: String(row.message_key ?? ''),
  direction: String(row.direction ?? ''),
  senderId: String(row.sender_id ?? ''),
  senderName: String(row.sender_name ?? ''),
  messageType: String(row.message_type ?? ''),
  content: String(row.content ?? ''),
  mediaDuration: Number(row.media_duration) || 0,
  status: String(row.status ?? ''),
  readStatus: Number(row.read_status) || 0,
  readAt: Number(row.read_at) || 0,
  sentAt: Number(row.sent_at) || 0,
});

/** 把运营会话 DTO 归一为 UI 模型。 */
const toOpsConversation = (row: {
  /** 会话主键。 */
  id?: number;
  /** 通道标识。 */
  channel?: string;
  /** 目标 openid。 */
  target_openid?: string;
  /** 是否群聊。 */
  is_group?: boolean;
  /** 最近消息毫秒时间。 */
  last_message_at?: number;
  /** 最近消息预览。 */
  last_message_preview?: string;
}): OpsConversation => ({
  id: Number(row.id) || 0,
  channel: String(row.channel ?? ''),
  targetOpenid: String(row.target_openid ?? ''),
  isGroup: row.is_group === true,
  lastMessageAt: Number(row.last_message_at) || 0,
  lastMessagePreview: String(row.last_message_preview ?? ''),
});

/** 把运营消息 DTO 归一为 UI 模型。 */
const toOpsMessage = (row: {
  /** 消息主键。 */
  id?: number;
  /** 会话主键。 */
  conversation_id?: number;
  /** 消息方向。 */
  direction?: string;
  /** 消息来源。 */
  source?: string;
  /** 纯文本内容。 */
  content?: string;
  /** 消息毫秒时间。 */
  created_at?: number;
}): OpsMessage => ({
  id: Number(row.id) || 0,
  conversationId: Number(row.conversation_id) || 0,
  direction: String(row.direction ?? ''),
  source: String(row.source ?? ''),
  content: String(row.content ?? ''),
  createdAt: Number(row.created_at) || 0,
});

/** 读取 AI 接管过的会话；游标由上一页最后一条的最近消息时间与会话标识组成。 */
export const getAgentTakeoverConversations = async (
  query: { /** 单页数量。 */ limit?: number; /** 上一页最后一条的最近消息毫秒时间。 */ beforeLastMessageAt?: number; /** 上一页最后一条的会话标识。 */ beforeChatId?: string } = {},
  options?: RequestControlOptions,
): Promise<AgentTakeoverConversationPage> => {
  // response 是 OpenAPI 约束的接管会话分页响应。
  const response = await runContractRequest(/* signal 控制接管会话读取的取消和超时。 */ signal => contractClient.GET('/api/v1/agent/support/conversations', {
    params: {
      query: {
        limit: query.limit,
        before_last_message_at: query.beforeLastMessageAt,
        before_chat_id: query.beforeChatId,
      },
    },
    signal,
  }), options);
  // sessions 是归一后的会话列表。
  const sessions = Array.isArray(response.sessions) ? response.sessions.map(toAgentTakeoverSession) : [];
  return {
    sessions,
    hasMore: response.has_more === true,
    nextBeforeLastMessageAt: typeof response.next_before_last_message_at === 'number' ? response.next_before_last_message_at : undefined,
    nextBeforeChatId: typeof response.next_before_chat_id === 'string' ? response.next_before_chat_id : undefined,
  };
};

/** 读取指定账号与会话的消息；复用聊天接口的归属校验，只读。 */
export const getAgentTakeoverMessages = async (
  query: { /** 账号标识。 */ accountId: string; /** 会话标识。 */ chatId: string; /** 更早消息游标。 */ beforeId?: number; /** 单页数量。 */ limit?: number },
  options?: RequestControlOptions,
): Promise<AgentConversationMessagePage> => {
  // response 是 OpenAPI 约束的聊天消息分页响应。
  const response = await runContractRequest(/* signal 控制接管会话消息读取的取消和超时。 */ signal => contractClient.GET('/api/v1/agent/support/conversations/messages', {
    params: {
      query: {
        account_id: query.accountId,
        chat_id: query.chatId,
        before_id: query.beforeId,
        limit: query.limit,
      },
    },
    signal,
  }), options);
  return {
    messages: Array.isArray(response.messages) ? response.messages.map(toAgentConversationMessage) : [],
    hasMore: response.has_more === true,
  };
};

/** 分页读取运营会话；按最近消息时间倒序。 */
export const getOpsConversations = async (
  query: { /** 从 1 开始的页码。 */ page?: number; /** 每页条数。 */ pageSize?: number } = {},
  options?: RequestControlOptions,
): Promise<OpsConversationPage> => {
  // response 是 OpenAPI 约束的运营会话分页响应。
  const response = await runContractRequest(/* signal 控制运营会话读取的取消和超时。 */ signal => contractClient.GET('/api/v1/agent/ops/conversations', {
    params: { query: { page: query.page, page_size: query.pageSize } },
    signal,
  }), options);
  // conversations 是归一后的会话列表。
  const conversations = Array.isArray(response.conversations) ? response.conversations.map(toOpsConversation) : [];
  return {
    conversations,
    total: Number(response.total) || 0,
    page: Number(response.page) || 1,
    pageSize: Number(response.page_size) || conversations.length,
    totalPages: Number(response.total_pages) || 0,
  };
};

/** 读取指定运营会话的消息；beforeId 用于加载更早消息。 */
export const getOpsMessages = async (
  conversationId: number,
  query: { /** 更早消息游标。 */ beforeId?: number; /** 单页数量。 */ limit?: number } = {},
  options?: RequestControlOptions,
): Promise<OpsMessagePage> => {
  // response 是 OpenAPI 约束的运营消息分页响应。
  const response = await runContractRequest(/* signal 控制运营消息读取的取消和超时。 */ signal => contractClient.GET('/api/v1/agent/ops/conversations/{id}/messages', {
    params: { path: { id: conversationId }, query: { before_id: query.beforeId, limit: query.limit } },
    signal,
  }), options);
  return {
    messages: Array.isArray(response.messages) ? response.messages.map(toOpsMessage) : [],
    hasMore: response.has_more === true,
  };
};

/** 从 Web 向运营会话发送一条人工文本；服务端先发 QQ，成功后落库并返回消息与会话摘要。 */
export const sendOpsMessage = async (conversationId: number, content: string, options?: RequestControlOptions): Promise<OpsSendMessageResult> => {
  // response 是 OpenAPI 约束的发送成功响应。
  const response = await runContractRequest(/* signal 控制运营消息发送的取消和超时。 */ signal => contractClient.POST('/api/v1/agent/ops/conversations/{id}/messages', {
    params: { path: { id: conversationId } },
    body: { content },
    signal,
  }), options);
  return {
    message: toOpsMessage(response.message),
    conversation: toOpsConversation(response.conversation),
  };
};
