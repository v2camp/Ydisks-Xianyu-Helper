import { Bell,Mail,MessageCircle,Send as Telegram,Webhook } from 'lucide-react';
import type { NotificationChannel,NotificationChannelType,NotificationEventType,SystemSettings } from './api';
import type { ConnectorMeta } from './registry';
import type { NotificationChannelMeta,NotificationEventMeta,NotificationForm,NotificationPayload } from './types';
import { normalizeNotificationEventTypes } from './models';

// notificationChannelTypes 是所有通知渠道的静态字段、图标和使用指南。
export const notificationChannelTypes: Record<NotificationChannelType, NotificationChannelMeta> = {
  bark: {
    label: 'Bark', icon: Bell,
    credential: { keys: ['device_key'] },
    fields: [
      { key: 'server_url', label: 'Bark 服务器', placeholder: 'https://api.day.app', required: true },
      { key: 'device_key', label: 'Device Key', placeholder: '你的 Bark 设备 Key', sensitive: true, required: true },
      { key: 'title', label: '标题（可选）', placeholder: '闲鱼助手' },
      { key: 'sound', label: '铃声（可选）', placeholder: 'default' },
      { key: 'group', label: '分组（可选）', placeholder: 'xianyu' },
    ],
    guide: { steps: ['App Store 搜索并安装 Bark（免费）', '打开 Bark App，首页会显示一条测试 URL，形如 https://api.day.app/<key>/这是测试推送内容', 'URL 路径里 <key> 那一段就是 Device Key，复制填入', '服务器地址默认 https://api.day.app，自建服务端则填自建域名'], urlFormat: 'https://api.day.app/<你的key>/测试内容', note: '不需要 secret，Device Key 本身就是唯一凭证。' },
  },
  dingtalk: {
    label: '钉钉机器人', icon: MessageCircle,
    credential: { keys: ['webhook_url', 'secret'] },
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=...', sensitive: true, required: true },
      { key: 'secret', label: '加签密钥', placeholder: 'SEC...', type: 'password', sensitive: true, help: '安全设置勾选「加签」后生成，SEC 开头' },
    ],
    guide: { steps: ['钉钉进入一个群（可建单人群）→ 右上角 群设置', '机器人 → 添加机器人 → 选择「自定义机器人」', '安全设置勾选「加签」', '完成页同时展示 Webhook 地址和 SEC 开头的 Secret，两个都复制', 'Webhook URL 填上面，Secret 填加签密钥'], urlFormat: 'https://oapi.dingtalk.com/robot/send?access_token=XXX', note: '加签密钥可选，但强烈建议启用，否则机器人可能被滥用。' },
  },
  feishu: {
    label: '飞书机器人', icon: MessageCircle,
    credential: { keys: ['webhook_url', 'secret'] },
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/...', sensitive: true, required: true },
      { key: 'secret', label: '签名校验密钥', placeholder: 'sec-...', type: 'password', sensitive: true, help: '安全设置选「签名校验」后生成' },
    ],
    guide: { steps: ['飞书进入目标群 → 右上角 更多 → 设置', '群机器人 → 添加机器人 → 自定义机器人', '安全设置选择「签名校验」，复制生成的秘钥', '完成会给 Webhook 地址，复制', 'Webhook URL 填上面，秘钥填签名校验密钥'], urlFormat: 'https://open.feishu.cn/open-apis/bot/v2/hook/xxx', note: '签名密钥可选，建议启用。频率限制：单机器人 100 次/分钟。' },
  },
  wechat: {
    label: '企业微信机器人', icon: MessageCircle,
    credential: { keys: ['webhook_url'] },
    fields: [{ key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...', sensitive: true, required: true }],
    guide: { steps: ['企业微信进入一个内部群 → 右上角 … → 群设置', '找到「群机器人」（新版可能叫「消息推送」）', '添加 → 选择自定义机器人', '完成给 Webhook 地址（含 ?key=），复制填入'], urlFormat: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=XXX', note: '不需要 secret，URL 里的 key 就是唯一凭证，注意不要泄露。频率限制 20 条/分钟。' },
  },
  webhook: {
    label: '自定义 Webhook', icon: Webhook,
    credential: { keys: ['webhook_url'] },
    fields: [{ key: 'webhook_url', label: 'Webhook URL', placeholder: 'https://your-server/notify', sensitive: true, required: true }],
    guide: { steps: ['仅适用于自己有服务器的用户：在你自己的机器上起一个 HTTP 接口收 JSON', '系统会 POST 这个 URL，Content-Type: application/json', '请求体固定为右侧 JSON 格式，按需解析 message 字段'], urlFormat: '{"message":"告警正文","timestamp":"2026-07-05 12:00:00","source":"xianyu-auto-reply"}', note: '若用 Server酱 / PushPlus 等第三方推送服务，格式不兼容，需写中间脚本转发，或改用对应专用渠道。' },
  },
  telegram: {
    label: 'Telegram', icon: Telegram,
    credential: { keys: ['bot_token'] },
    fields: [{ key: 'bot_token', label: 'Bot Token', placeholder: '123456:ABC-DEF...', sensitive: true, required: true }, { key: 'chat_id', label: 'Chat ID', placeholder: '-1001234567890 或你的用户 ID', required: true }],
    guide: { steps: ['在 Telegram 搜索 @BotFather，发送 /newbot，按提示创建机器人，拿到 Bot Token', '把你的 Bot 拉进一个群，或私聊它发一条消息', '浏览器访问 https://api.telegram.org/bot<你的Token>/getUpdates', '从返回 JSON 里找 "chat":{"id":xxx}，xxx 就是 Chat ID（群是负数，私聊是正数）'], urlFormat: 'Bot Token: 123456:ABC-DEF...  Chat ID: -1001234567890', note: '群聊需先把 Bot 设为管理员才能发消息。' },
  },
  email: {
    label: '邮件', icon: Mail,
    credential: { keys: [] },
    fields: [{ key: 'to_email', label: '收件邮箱', placeholder: 'receiver@example.com', required: true }],
    guide: { steps: ['先在本页「当前用户的集成配置 → 邮件发送（SMTP）」保存发件邮箱与授权码', '邮件渠道只需填写收件邮箱，会使用当前用户的 SMTP 配置发信'], note: '邮件渠道不再支持渠道级独立 SMTP；更换发件服务请修改当前用户的 SMTP 配置。' },
  },
  qq: {
    label: 'QQ 机器人', icon: MessageCircle,
    credential: { keys: [] },
    fields: [
      { key: 'user_openid', label: '单聊目标 OpenID', placeholder: '接收通知的用户 openid（与 AppID 绑定）', help: '单聊与群聊至少填一个，单聊优先' },
      { key: 'group_openid', label: '群聊目标 OpenID', placeholder: '接收通知的群 openid（与 AppID 绑定）', help: '与单聊至少填一个' },
    ],
    guide: { steps: ['先在本页「当前用户的集成配置 → QQ 机器人」填写 AppID 与 AppSecret 并保存', '把机器人拉进目标群，或让用户与机器人单聊发一条消息', '在管理页或调用接口获取 user_openid / group_openid（openid 与 AppID 绑定，换机器人需重配）'], urlFormat: 'AppID / AppSecret 在本页「当前用户的集成配置」中配置', note: '主动推送有每日限额且需审核；openid 不可跨 AppID 复用，换机器人后需重配。' },
  },
};

// notificationEvents 是可绑定到通知渠道的事件定义。
export const notificationEvents: NotificationEventMeta[] = [
  { value: 'account_offline', label: '掉线通知', description: '账号过期、断线或登录态失效' },
  { value: 'account_recovered', label: '恢复通知', description: '自动恢复成功并重新在线' },
  { value: 'account_disabled', label: '禁用通知', description: '连续失败、账密错误等导致账号停用' },
  { value: 'security_verification', label: '风控验证', description: '滑块、人脸、扫码验证等安全校验' },
  { value: 'automation_order_created', label: '拍下改价', description: '买家拍下订单后，自动改价任务的成功或失败结果' },
  { value: 'automation_order_paid', label: '付款发货', description: '买家付款后，自动发货任务的成功或失败结果' },
  { value: 'automation_buyer_reviewed', label: '评价赠品', description: '买家评价后，评价赠品任务的成功或失败结果' },
  { value: 'automation_review_missing_timeout', label: '求评价', description: '订单超时未评价时，求评价任务的成功或失败结果' },
  { value: 'manual_delivery_result', label: '手动发货结果', description: '人工发货或确认发货的结果，不属于自动化任务分类' },
  { value: 'manual_intervention_required', label: '需要人工处理', description: '自动化因结果不确定、状态缺失或无法安全续跑而停止，需要用户检查订单' },
  { value: 'token_renewal', label: '续期通知', description: 'Cookie/token 续期和自动恢复过程' },
  { value: 'system_error', label: '账号异常（未分类）', description: '账号级未知异常；实例级告警在系统告警页单独配置' },
];

// systemAlertEvents 是管理员系统告警渠道可订阅的实例级事件。
export const systemAlertEvents: NotificationEventMeta[] = [
  { value: 'business_silence', label: '业务静默', description: '进程健康但业务表长时间没有新事件' },
];

// notificationEventLabel 返回事件编码对应的中文名称；未知编码回退为原值。
export const notificationEventLabel = (eventType: string): string =>
  [...notificationEvents, ...systemAlertEvents].find(
    // item 是当前待匹配的事件定义。
    item => item.value === eventType,
  )?.label || eventType;

// notificationLevelLabel 返回事件级别的中文名称；未知级别回退为原值。
export const notificationLevelLabel = (level: string): string => {
  switch (level) {
    case 'critical':
      return '严重';
    case 'warn':
      return '警告';
    case 'info':
      return '提示';
    default:
      return level || '提示';
  }
};

// notificationEventTime 把 Unix 秒时间戳格式化为本地时间文本。
export const notificationEventTime = (createdAt: number): string => {
  if (!createdAt) return '';
  return new Date(createdAt * 1000).toLocaleString('zh-CN', { hour12: false });
};

// emptyNotificationForm 返回新建渠道的初始表单。
export const emptyNotificationForm = (): NotificationForm => ({ name: '', type: 'bark', enabled: true, config: {}, event_types: [] });

// notificationEventSummary 将事件值转换为渠道列表中的中文摘要。
export const notificationEventSummary = (events?: NotificationEventType[]): string => {
  if (!events || events.length === 0) return '全部事件';
  return events.map(
    // event 是当前待转换的后端事件值。
    event => notificationEventLabel(event),
  ).join('、');
};

// normalizeNotificationForm 将编辑中的渠道配置复制为独立表单对象；渠道配置不再包含任何连接器级凭据。
export const normalizeNotificationForm = (channel: NotificationChannel): NotificationForm => ({
  name: channel.name,
  type: channel.type,
  enabled: channel.enabled,
  config: { ...(channel.config || {}) },
  event_types: normalizeNotificationEventTypes(channel.event_types || []),
});

// credentialKeysOf 返回某渠道类型声明的凭据键；只有这些键参与必填豁免与显式清除，避免豁免外溢到普通配置。
export const credentialKeysOf = (type: NotificationChannelType): string[] => notificationChannelTypes[type].credential.keys;

// ChannelCredentialOptions 描述当前渠道已保存的凭据，用于决定凭据键是否仍需填写。
export interface ChannelCredentialOptions {
  // savedCredentialKeys 是渠道自身已保存的凭据键名，留空保存会沿用服务端原值。
  savedCredentialKeys?: string[];
}

// validateNotificationForm 校验渠道名称与渠道字段必填项。
export const validateNotificationForm = (form: NotificationForm, options: ChannelCredentialOptions = {}): string => {
  // meta 是当前渠道类型的静态校验配置。
  const meta = notificationChannelTypes[form.type];
  // exemptKeys 是本次无需重填的凭据键：只从当前类型声明的凭据键中挑选，其他字段一律保持必填。
  const exemptKeys = meta.credential.keys.filter(
    // key 是当前待判定的凭据键。
    key => options.savedCredentialKeys?.includes(key) === true,
  );
  // missingField 是第一个未填写的渠道必填字段。
  const missingField = meta.fields.find(
    // field 是当前渠道字段定义。
    field => field.required && !exemptKeys.includes(field.key) && !String(form.config[field.key] || '').trim(),
  );
  if (missingField) return `请填写 ${missingField.label}`;
  if (!form.name.trim()) return '请填写渠道名称';
  return '';
};

// buildChannelConfig 生成非邮件渠道的提交配置：显式清除时把该类型声明的凭据键写成空串，缺省则原样提交让服务端保留原值。
const buildChannelConfig = (form: NotificationForm): Record<string, unknown> => {
  if (form.credentialAction !== 'clear') return form.config;
  // config 是本次提交的配置副本，避免把删除标记写回正在编辑的表单状态。
  const config = { ...form.config };
  for (const /* key 是当前待清空的凭据键。 */ key of credentialKeysOf(form.type)) config[key] = '';
  return config;
};

// buildNotificationPayload 将表单转换为通知渠道保存请求；邮件渠道只提交收件地址。
export const buildNotificationPayload = (form: NotificationForm): NotificationPayload => ({
  name: form.name.trim(),
  type: form.type,
  config: form.type === 'email'
    ? { to_email: String(form.config.to_email ?? '').trim() }
    : buildChannelConfig(form),
  event_types: form.event_types,
  enabled: form.enabled,
});

// isCurrentNotificationRequest 判断异步通知请求是否仍属于最新代次。
export const isCurrentNotificationRequest = (generation: number, currentGeneration: number): boolean => generation === currentGeneration;

// notificationErrorMessage 将未知异常转换为用户可读的通知错误。
export const notificationErrorMessage = (error: unknown, fallback: string): string => error instanceof Error && error.message ? error.message : fallback;

// connectorReady 判断连接器草稿是否已具备全部必需凭据：非敏感字段要求非空，敏感字段要求服务端已配置或本次新填。
export const connectorReady = (connector: ConnectorMeta, settings: SystemSettings): boolean => connector.fields
  .filter(/* requiredFilter 只保留参与就绪判定的必需字段。 */ field => field.required === true)
  .every(/* readinessPredicate 判断单个必需字段在草稿中是否已具备。 */ field => {
    if (field.sensitive) {
      // configured 表示服务端已保存该敏感值，草稿不回显明文时仍视为具备。
      const configured = field.configuredKey ? settings[field.configuredKey] === true : false;
      return configured || String(settings[field.key] ?? '').trim() !== '';
    }
    return String(settings[field.key] ?? '').trim() !== '';
  });

// isAIReady 判断当前用户的集成配置是否已具备可用的 AI 模型与密钥。
export const isAIReady = (settings: SystemSettings): boolean =>
  String(settings.ai_model || '').trim() !== '' && settings.ai_api_key_configured === true;
