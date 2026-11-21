import type { MutationIDResponse,NotificationChannel,NotificationChannelEditorResponse,NotificationChannelResponse,NotificationEvent,NotificationEventList,NotificationEventType,OperationResponse } from './models';
import { type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { normalizeNotificationEventTypes } from './models';
export type * from './models';
/** ChannelRequestOptions 是渠道请求使用的取消与超时控制选项。 */
export type ChannelRequestOptions = RequestControlOptions;
// 连接器注册表需要的设置键常量：契约层是唯一事实源，feature 内一律经本适配器转出。
export { QQ_CONNECTOR_READY_KEYS } from '../../../shared/api-contract/settings';
// 用户级 AI 模型操作经本适配器转出，避免 UI 直接依赖 transport DTO。
export { fetchUserAIModels,testUserAIConnection } from '../../../shared/api-contract/userIntegrations';

/** 通知渠道写入时使用的具名请求 DTO。 */
export interface NotificationChannelRequest {
  /** 管理界面展示的渠道名称。 */
  name?: string;
  /** 后端支持的渠道类型。 */
  type?: string;
  /** 由各渠道表单构造的非敏感配置。 */
  config?: Record<string, unknown>;
  /** 需要订阅的系统事件。 */
  event_types?: NotificationEventType[];
  /** 渠道是否参与通知投递。 */
  enabled?: boolean;
}

/** 将后端字符串或历史分隔文本转换为稳定事件类型列表。 */
const parseNotificationEventTypes = (raw: unknown): NotificationEventType[] => {
  if (Array.isArray(raw)) return normalizeNotificationEventTypes(raw.filter(Boolean) as NotificationEventType[]);
  if (typeof raw !== 'string' || !raw.trim()) return [];
  try {
    // parsed 是解析后的历史 JSON 事件列表。
    const parsed: unknown = JSON.parse(raw);
    if (Array.isArray(parsed)) return normalizeNotificationEventTypes(parsed.filter(Boolean) as NotificationEventType[]);
  } catch {
    // 非 JSON 历史值继续按分隔符兼容解析。
  }
  return normalizeNotificationEventTypes(raw.split(/[,\s;]+/).map(value => value.trim()).filter(Boolean) as NotificationEventType[]);
};

/** 将事件类型列表序列化为后端稳定保存的 JSON 文本。 */
const stringifyNotificationEventTypes = (eventTypes?: NotificationEventType[]): string => {
  // uniqueTypes 是去重且去除空值后的事件列表。
  const uniqueTypes = Array.from(new Set((eventTypes || []).filter(Boolean)));
  return uniqueTypes.length > 0 ? JSON.stringify(uniqueTypes) : '';
};

/** 把后端渠道 DTO 转换为通知编辑器使用的 UI 模型。 */
const toNotificationChannel = (channel: NotificationChannelResponse): NotificationChannel => {
	// config 是列表摘要不返回 SMTP 等秘密时使用的空编辑初始值。
	const config: Record<string, unknown> = {};
  // type 是处理 ding_talk/lark 历史别名后的渠道类型。
  const type = (channel.type === 'ding_talk' ? 'dingtalk' : channel.type === 'lark' ? 'feishu' : channel.type) as NotificationChannel['type'];
	return { id: String(channel.id), name: channel.name, type, config, event_types: parseNotificationEventTypes(channel.event_types), enabled: channel.enabled };
};

/** 获取全部通知渠道并转换为编辑器模型。 */
export const getNotificationChannels = async (options?: RequestControlOptions): Promise<{ /** 操作是否完成。 */ success: boolean; /** 渠道列表。 */ data: NotificationChannel[] }> => {
  // response 是渠道列表的原始传输响应。
  const response = await runContractRequest(/* signal 控制通知渠道列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/notifications/channels', { signal }), options);
  return { success: true, data: response.map(toNotificationChannel) };
};

/** 获取单个通知渠道编辑所需的脱敏配置。 */
export const getNotificationChannel = async (channelID: string, options?: RequestControlOptions): Promise<NotificationChannelEditorResponse> => runContractRequest(/* signal 控制通知渠道编辑配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/notifications/channels/{channel_id}', { params: { path: { channel_id: channelID } }, signal }), options);

/** 创建通知渠道，并在传输边界序列化配置与事件。 */
export const createNotificationChannel = async (data: Required<Pick<NotificationChannelRequest, 'name' | 'type' | 'config'>> & NotificationChannelRequest, options?: RequestControlOptions): Promise<MutationIDResponse> => runContractRequest(/* signal 控制通知渠道创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/notifications/channels', { body: { name: data.name, type: data.type, config: JSON.stringify(data.config), event_types: stringifyNotificationEventTypes(data.event_types), enabled: data.enabled }, signal }), options);

/** 更新通知渠道的可编辑字段。 */
export const updateNotificationChannel = async (channelID: string, data: NotificationChannelRequest, options?: RequestControlOptions): Promise<OperationResponse> => {
  // payload 是已完成配置和事件序列化的更新请求。
  const payload = {
    name: data.name,
    type: data.type,
    config: data.config === undefined ? undefined : JSON.stringify(data.config),
    event_types: data.event_types === undefined ? undefined : stringifyNotificationEventTypes(data.event_types),
    enabled: data.enabled,
  };
  return runContractRequest(/* signal 控制通知渠道更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/notifications/channels/{channel_id}', { params: { path: { channel_id: channelID } }, body: payload, signal }), options);
};

/** 删除指定通知渠道。 */
export const deleteNotificationChannel = async (channelID: string, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制通知渠道删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/notifications/channels/{channel_id}', { params: { path: { channel_id: channelID } }, signal }), options);

/** 向单一通知渠道发送测试投递。 */
export const testNotificationChannel = async (channelID: string, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制通知渠道测试投递的取消和超时。 */ signal => contractClient.POST('/api/v1/notifications/channels/{channel_id}/test', { params: { path: { channel_id: channelID } }, signal }), options);

/** 获取管理员系统告警渠道并转换为编辑器模型。 */
export const getSystemAlertChannels = async (options?: RequestControlOptions): Promise<{ /** 操作是否完成。 */ success: boolean; /** 系统告警渠道列表。 */ data: NotificationChannel[] }> => {
  // response 是系统告警渠道列表的原始传输响应。
  const response = await runContractRequest(/* signal 控制系统告警渠道列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/admin/system-alert-channels', { signal }), options);
  return { success: true, data: response.map(toNotificationChannel) };
};

/** 获取单个系统告警渠道编辑所需的脱敏配置。 */
export const getSystemAlertChannel = async (channelID: string, options?: RequestControlOptions): Promise<NotificationChannelEditorResponse> => runContractRequest(/* signal 控制系统告警渠道编辑配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/admin/system-alert-channels/{channel_id}', { params: { path: { channel_id: channelID } }, signal }), options);

/** 创建系统告警渠道。 */
export const createSystemAlertChannel = async (data: Required<Pick<NotificationChannelRequest, 'name' | 'type' | 'config'>> & NotificationChannelRequest, options?: RequestControlOptions): Promise<MutationIDResponse> => runContractRequest(/* signal 控制系统告警渠道创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/admin/system-alert-channels', { body: { name: data.name, type: data.type, config: JSON.stringify(data.config), event_types: stringifyNotificationEventTypes(data.event_types), enabled: data.enabled }, signal }), options);

/** 更新系统告警渠道的可编辑字段。 */
export const updateSystemAlertChannel = async (channelID: string, data: NotificationChannelRequest, options?: RequestControlOptions): Promise<OperationResponse> => {
  // payload 是已完成配置和事件序列化的更新请求。
  const payload = {
    name: data.name,
    type: data.type,
    config: data.config === undefined ? undefined : JSON.stringify(data.config),
    event_types: data.event_types === undefined ? undefined : stringifyNotificationEventTypes(data.event_types),
    enabled: data.enabled,
  };
  return runContractRequest(/* signal 控制系统告警渠道更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/admin/system-alert-channels/{channel_id}', { params: { path: { channel_id: channelID } }, body: payload, signal }), options);
};

/** 删除系统告警渠道。 */
export const deleteSystemAlertChannel = async (channelID: string, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制系统告警渠道删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/admin/system-alert-channels/{channel_id}', { params: { path: { channel_id: channelID } }, signal }), options);

/** 向系统告警渠道发送测试投递。 */
export const testSystemAlertChannel = async (channelID: string, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制系统告警渠道测试投递的取消和超时。 */ signal => contractClient.POST('/api/v1/admin/system-alert-channels/{channel_id}/test', { params: { path: { channel_id: channelID } }, signal }), options);

// NotificationEventTransport 是事件接口的最小传输结构；只在本适配器内使用。
interface NotificationEventTransport {
  /** 事件主键。 */
  id: number;
  /** 事件作用域。 */
  scope: string;
  /** 租户事件关联账号。 */
  account_id?: string;
  /** 事件编码。 */
  event_type: string;
  /** 事件级别。 */
  level: string;
  /** 事件标题。 */
  title: string;
  /** 事件正文。 */
  body: string;
  /** 是否已读。 */
  read: boolean;
  /** 事件发生时间。 */
  created_at: number;
}

// toNotificationEventList 把传输事件转换为站内通知中心 UI 模型。
const toNotificationEventList = (raw: { /** 事件列表。 */ items?: NotificationEventTransport[]; /** 未读数。 */ unread_count?: number }): NotificationEventList => ({
  items: (raw.items || []).map(
    // event 是当前待转换的传输事件。
    (event): NotificationEvent => ({
      id: String(event.id),
      scope: event.scope === 'system' ? 'system' : 'tenant',
      account_id: event.account_id || '',
      event_type: event.event_type,
      level: event.level,
      title: event.title,
      body: event.body,
      read: event.read,
      created_at: event.created_at,
    }),
  ),
  unread_count: raw.unread_count || 0,
});

/** 获取当前用户的租户站内通知事件。 */
export const getNotificationEvents = async (limit = 50, options?: RequestControlOptions): Promise<NotificationEventList> => {
  // response 是租户事件列表的原始传输响应。
  const response = await runContractRequest(/* signal 控制租户通知事件读取的取消和超时。 */ signal => contractClient.GET('/api/v1/notifications/events', { params: { query: { limit } }, signal }), options);
  return toNotificationEventList(response);
};

/** 标记当前用户的单条租户事件已读。 */
export const markNotificationEventRead = async (eventID: string, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制租户事件已读请求的取消和超时。 */ signal => contractClient.POST('/api/v1/notifications/events/{event_id}/read', { params: { path: { event_id: eventID } }, signal }), options);

/** 标记当前用户全部租户事件已读。 */
export const markAllNotificationEventsRead = async (options?: RequestControlOptions): Promise<OperationResponse & { /** 实际更新数量。 */ updated?: number }> => runContractRequest(/* signal 控制租户全部已读请求的取消和超时。 */ signal => contractClient.POST('/api/v1/notifications/events/read-all', { signal }), options);

/** 获取管理员可见的系统级站内通知事件。 */
export const getSystemNotificationEvents = async (limit = 50, options?: RequestControlOptions): Promise<NotificationEventList> => {
  // response 是系统事件列表的原始传输响应。
  const response = await runContractRequest(/* signal 控制系统通知事件读取的取消和超时。 */ signal => contractClient.GET('/api/v1/admin/notifications/system-events', { params: { query: { limit } }, signal }), options);
  return toNotificationEventList(response);
};

/** 标记单条系统级事件已读。 */
export const markSystemNotificationEventRead = async (eventID: string, options?: RequestControlOptions): Promise<OperationResponse> => runContractRequest(/* signal 控制系统事件已读请求的取消和超时。 */ signal => contractClient.POST('/api/v1/admin/notifications/system-events/{event_id}/read', { params: { path: { event_id: eventID } }, signal }), options);

/** 标记全部系统级事件已读。 */
export const markAllSystemNotificationEventsRead = async (options?: RequestControlOptions): Promise<OperationResponse & { /** 实际更新数量。 */ updated?: number }> => runContractRequest(/* signal 控制系统全部已读请求的取消和超时。 */ signal => contractClient.POST('/api/v1/admin/notifications/system-events/read-all', { signal }), options);
