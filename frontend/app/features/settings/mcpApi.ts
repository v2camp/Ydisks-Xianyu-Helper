import { contractClient,runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import type { MCPAuditPage,MCPAuditRecord,MCPServiceStatus,OperationResponse } from './models';

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
