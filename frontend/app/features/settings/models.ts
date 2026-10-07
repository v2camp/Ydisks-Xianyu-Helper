// models 保存本 feature adapter 输出的 UI 模型；这些模型不直接代表 HTTP DTO。
/** 由当前 feature adapter 归一后的 SystemSettings UI 模型；不直接暴露 HTTP DTO。 */
export interface SystemSettings {
  /** 默认 AI 模型名称。 */
  ai_model?: string;
  /** 全局 AI API 密钥。 */
  ai_api_key?: string;
  /** 全局 AI API 密钥是否已在服务端配置。 */
  ai_api_key_configured?: boolean;
  /** 全局 AI API 地址。 */
  ai_api_url?: string;
  /** 全局 AI 基础地址。 */
  ai_base_url?: string;
  /** 系统默认回复文案。 */
  default_reply?: string;
  /** 是否允许注册新用户。 */
  registration_enabled?: boolean;
  /** 系统 SMTP 服务器地址。 */
  smtp_server?: string;
  /** 系统 SMTP 密码是否已在服务端配置。 */
  smtp_password_configured?: boolean;
  /** 服务端日志级别。 */
  log_level?: 'debug' | 'info' | 'warn' | 'error' | string;
  /** 服务端日志格式。 */
  log_format?: 'text' | 'json' | string;
  /** 是否阻止用户配置的服务端 HTTP 请求访问内网地址。 */
  outbound_http_public_only?: boolean;
  /** 续期日志保留天数。 */
  renewal_log_retention_days?: number;
  /** 远程验证码服务地址。 */
  'captcha.remote_service_url'?: string;
  /** 远程验证码服务密钥。 */
  'captcha.remote_secret_key'?: string;
  /** 远程验证码服务密钥是否已在服务端配置。 */
  'captcha.remote_secret_key_configured'?: boolean;
  /** 远程验证码服务 Cookie 配置。 */
  'captcha.remote_pass_cookies'?: boolean | string;
  /** 边界配置 JSON：意图白名单与负向组合词，决定 AI 接管范围。 */
  ai_scope_config?: string;
  /** 语料配置 JSON：FAQ 问答（在售商品与库存由系统自动查询注入），供 AI 有据作答。 */
  ai_knowledge_config?: string;
  /** 策略配置 JSON：报价与拒绝兜底话术模板。 */
  ai_policy_config?: string;
  /** 是否启用 AI 回复人工确认模式，开启后 AI 回复需人工确认后再发送。 */
  ai_reply_review_mode?: boolean;
  /** 多账号合计的每日发送条数上限，0 表示不限制。 */
  global_send_daily_limit?: number;
  /** 业务静默告警阈值（分钟），0 表示关闭看门狗，未配置回落默认 180。 */
  silence_alert_minutes?: number;
  /** QQ 连接器机器人的 AppID，来自 QQ 开放平台，明文保存且可被通知渠道继承。 */
  'qqbot.app_id'?: string;
  /** QQ 连接器机器人的 AppSecret 草稿，仅存在于提交请求中，服务端不回显明文。 */
  'qqbot.app_secret'?: string;
  /** QQ 连接器机器人的 AppSecret 是否已在服务端配置。 */
  'qqbot.app_secret_configured'?: boolean;
  /** MCP 服务器列表 JSON 字符串，元素为含 name 与 url 的对象；引擎消费其中 find_stuff 条目。 */
  'mcp.servers'?: string;
  /** 发货内容门禁 JSON 字符串：enabled 违禁词门禁、link_check 链接健康检查、extra_block_words 额外违禁词。 */
  delivery_content_guard?: string;
  /** 兼容未来配置键的扩展字段。 */
  /** 未知设置键只能承载服务端声明的标量值，敏感值不进入该 UI 模型。 */
  [key: string]: string | number | boolean | undefined;
}

/** 由当前 feature adapter 归一后的 AIReplySettings UI 模型；不直接暴露 HTTP DTO。 */
export interface AIReplySettings {
  /** 是否启用账号 AI 回复。 */
  ai_enabled: boolean;
  /** 最大折扣比例。 */
  max_discount_percent: number;
  /** 最大折扣金额。 */
  max_discount_amount?: number;
  /** 最大砍价轮次。 */
  max_bargain_rounds: number;
  /** 自定义提示词。 */
  custom_prompts: string;
}

/** AI 模型发现接口的具名响应。 */
/** 由当前 feature adapter 归一后的 AIModelsResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface AIModelsResponse {
  /** 远端可用模型名称。 */
  models: string[];
}

/** 简单资源创建接口的数值主键响应。 */
/** 由当前 feature adapter 归一后的 MutationIDResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface MutationIDResponse {
  /** 资源创建是否完成。 */
  success: boolean;
  /** 新资源数值主键。 */
  id: number;
}

/** 简单变更接口的统一成功响应。 */
/** 由当前 feature adapter 归一后的 OperationResponse UI 模型；不直接暴露 HTTP DTO。 */
export interface OperationResponse {
  /** 操作是否完成。 */
  success: boolean;
  /** 可选的操作说明。 */
  message?: string;
  /** 操作完成后是否需要重新登录。 */
  requires_relogin?: boolean;
}

/** MCP 对外服务状态 UI 模型；来自管理接口，不包含令牌明文或哈希。 */
export interface MCPServiceStatus {
  /** 服务是否已启用。 */
  enabled: boolean;
  /** 是否显式放行非本机来源访问。 */
  allowNonLoopback: boolean;
  /** 当前是否存在可用持久化令牌（含宽限期内旧令牌）。 */
  hasToken: boolean;
  /** 当前令牌创建时间的 Unix 秒，无令牌为 0。 */
  tokenCreatedAt: number;
  /** 当前令牌最近一次鉴权成功时间的 Unix 秒，从未使用为 0。 */
  tokenLastUsedAt: number;
  /** 是否存在宽限期内的旧令牌。 */
  hasPreviousToken: boolean;
  /** 宽限旧令牌失效时刻的 Unix 秒，无旧令牌为 0。 */
  previousTokenExpiresAt: number;
  /** Harness 应使用的接入地址。 */
  endpoint: string;
}

/** 一条 MCP 调用审计 UI 模型；参数摘要已由服务端按白名单键级脱敏。 */
export interface MCPAuditRecord {
  /** 审计行主键。 */
  id: number;
  /** 调用发生时间的 Unix 秒。 */
  createdAt: number;
  /** 执行调用的固定管理员本地用户标识。 */
  userId: number;
  /** 本次调用的令牌来源（persisted/environment）。 */
  tokenSource: string;
  /** 调用类别：tool、resource 或 prompt。 */
  category: string;
  /** 被调用的工具、资源或提示名称。 */
  name: string;
  /** 调用目标账号标识；无账号归属时为空串。 */
  cookieId: string;
  /** 键级脱敏后的参数 JSON 摘要。 */
  arguments: string;
  /** 调用是否成功完成。 */
  success: boolean;
  /** 失败类别的稳定标识；成功时为空串。 */
  errorClass: string;
  /** 调用耗时（毫秒）。 */
  durationMs: number;
}

/** MCP 调用审计分页 UI 模型。 */
export interface MCPAuditPage {
  /** 当前页审计记录。 */
  records: MCPAuditRecord[];
  /** 满足筛选条件的总记录数。 */
  total: number;
  /** 当前页码。 */
  page: number;
  /** 当前页大小。 */
  pageSize: number;
  /** 总页数。 */
  totalPages: number;
}

/** MCP 卡片操作结果提示。 */
export interface MCPServiceMessage {
  /** 提示类型：成功或失败。 */
  type: 'success' | 'error';
  /** 直接展示给管理员的中文提示文本。 */
  text: string;
}
