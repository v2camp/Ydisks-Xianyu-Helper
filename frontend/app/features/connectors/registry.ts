import { Bot } from 'lucide-react';
import type { ElementType } from 'react';
import type { NotificationChannelType } from './models';

/** QQ_OPEN_PLATFORM_URL 是管理员创建机器人并获取 AppID 与 AppSecret 的 QQ 开放平台入口。 */
export const QQ_OPEN_PLATFORM_URL = 'https://q.qq.com/';

/** QQ_CONNECTOR_GUIDE_STEPS 按顺序列出在 QQ 开放平台创建官方机器人并取回凭据的步骤。 */
export const QQ_CONNECTOR_GUIDE_STEPS: string[] = [
  '打开 QQ 开放平台 q.qq.com，用手机 QQ 扫码登录（官方机器人通道，无需第三方协议端）',
  '进入「机器人」→ 创建机器人，填写名称、简介与头像后提交',
  '在机器人的「开发设置」页复制 AppID 与 AppSecret（AppSecret 仅展示一次，请立即保存）',
  '回到本页填入 AppID 与 AppSecret 并保存，再到「通知渠道」新建 QQ 机器人渠道即可发送',
];

/** ConnectorFieldType 是连接器凭据字段支持的输入控件类型。 */
export type ConnectorFieldType = 'text' | 'password' | 'textarea' | 'number';

/** ConnectorField 描述连接器的一个系统级配置字段；key 直接对应系统设置键。 */
export interface ConnectorField {
  /** key 是系统设置键名，例如 qqbot.app_id。 */
  key: string;
  /** label 是字段在卡片中的中文名称。 */
  label: string;
  /** placeholder 是输入框的示例提示。 */
  placeholder?: string;
  /** type 是输入控件类型，缺省为单行文本。 */
  type?: ConnectorFieldType;
  /** sensitive 表示该键是否走敏感三态写入，明文不回显。 */
  sensitive?: boolean;
  /** configuredKey 是敏感键的服务端「已配置」派生标记键，用于回显配置状态。 */
  configuredKey?: string;
  /** configuredHelp 是敏感键已配置时展示的说明文案。 */
  configuredHelp?: string;
  /** required 表示该字段是否参与「凭据已就绪」判定。 */
  required?: boolean;
}

/** ConnectorCapability 描述连接器对外提供的一项能力及其开关键。 */
export interface ConnectorCapability {
  /** id 是能力稳定标识，用于生成控件标识与测试断言。 */
  id: string;
  /** label 是能力开关的中文名称。 */
  label: string;
  /** description 是能力开放范围与副作用的说明。 */
  description: string;
  /** toggleKey 是能力总开关的系统设置键。 */
  toggleKey: string;
  /** allowlistKey 是能力发送者白名单的系统设置键，缺省表示该能力没有白名单。 */
  allowlistKey?: string;
  /** allowlistLabel 是白名单输入框的中文名称。 */
  allowlistLabel?: string;
  /** allowlistPlaceholder 是白名单输入框的示例提示。 */
  allowlistPlaceholder?: string;
  /** allowlistHelp 是白名单的授权与安全说明。 */
  allowlistHelp?: string;
}

/** ConnectorGuide 描述如何在第三方平台获取该连接器的凭据。 */
export interface ConnectorGuide {
  /** title 是引导区块标题。 */
  title: string;
  /** steps 是按顺序展示的配置步骤。 */
  steps: string[];
  /** url 是第三方平台入口地址。 */
  url?: string;
  /** urlLabel 是第三方平台入口的按钮文案。 */
  urlLabel?: string;
}

/** ConnectorMeta 描述一种连接器的展示、凭据字段、能力与平台约束。 */
export interface ConnectorMeta {
  /** id 是连接器稳定标识，也是缓存与测试定位键。 */
  id: string;
  /** label 是连接器的中文名称，不含「连接器」后缀。 */
  label: string;
  /** icon 是连接器卡片标题旁的图标组件。 */
  icon: ElementType;
  /** summary 是连接器卡片标题下的用途说明。 */
  summary: string;
  /** fields 是连接器的系统级凭据字段定义。 */
  fields: ConnectorField[];
  /** capabilities 是连接器提供的能力开关定义。 */
  capabilities: ConnectorCapability[];
  /** guide 是可选的凭据获取引导，缺省时不展示引导区块。 */
  guide?: ConnectorGuide;
  /** notices 是连接器必须明示的平台限制与安全提示。 */
  notices?: string[];
  /** effectNote 是凭据与开关的生效时机说明。 */
  effectNote: string;
  /** powersChannelTypes 是该连接器支撑的通知渠道类型，用于渠道与连接器的对应关系。 */
  powersChannelTypes: NotificationChannelType[];
}

/** qqBotConnector 是 QQ 官方机器人连接器：系统级凭据供通知渠道复用，并承载入站只读命令。 */
export const qqBotConnector: ConnectorMeta = {
  id: 'qqbot',
  label: 'QQ 机器人',
  icon: Bot,
  summary: '在系统级配置一次 AppID / AppSecret，通知渠道里新建「QQ 机器人」即可直接复用，无需逐条重复填写。',
  fields: [
    { key: 'qqbot.app_id', label: '机器人 AppID', placeholder: '例如：1020xxxxx', required: true },
    {
      key: 'qqbot.app_secret', label: '机器人 AppSecret', type: 'password', sensitive: true,
      configuredKey: 'qqbot.app_secret_configured', required: true,
      configuredHelp: '服务端已保存 AppSecret 且仅本地加密存储，页面不会回显明文；留空保存表示保持原值。',
    },
  ],
  capabilities: [
    {
      id: 'inbound-commands',
      label: '启用入站命令',
      description: '开启后服务会连接 QQ 网关，接收「销量 / 健康 / 会话 / 帮助」四类只读命令并回复结果。关闭时不产生任何对外连接。',
      toggleKey: 'qqbot.commands_enabled',
      allowlistKey: 'qqbot.command_openids',
      allowlistLabel: '命令发送者白名单（openid）',
      allowlistPlaceholder: '留空表示拒绝所有发送者。每行一个或用逗号分隔，例如：\n0A1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P',
      allowlistHelp: '机器人一旦上线，任何找到它的人都能发消息；只有白名单内的 openid 才能触发命令。未授权者会收到含其 openid 的提示，把该标识粘贴到这里即可开通。',
    },
  ],
  guide: {
    title: '如何获取 AppID 与 AppSecret',
    steps: QQ_CONNECTOR_GUIDE_STEPS,
    url: QQ_OPEN_PLATFORM_URL,
    urlLabel: '前往 QQ 开放平台',
  },
  notices: [
    '官方机器人通道的主动推送有额度限制：同一用户或同一群每月仅 4 条主动消息，被动回复窗口为单聊 60 分钟、群聊 5 分钟。机器人的 openid 与 AppID 绑定，换机器人后通知渠道里的 openid 需重新获取。',
  ],
  effectNote: '凭据与开关在下次服务重启后生效；网关断开后服务会按指数退避自动重连，无需手动干预。',
  powersChannelTypes: ['qq'],
};

/** connectorTypes 是连接器页按顺序渲染的全部连接器定义；新增连接器只需在此追加条目。 */
export const connectorTypes: ConnectorMeta[] = [qqBotConnector];

/** connectorField 描述连接器写入系统设置时的一个键及其敏感语义。 */
export interface ConnectorSettingsKey {
  /** key 是系统设置键名。 */
  key: string;
  /** sensitive 表示该键是否走敏感三态写入。 */
  sensitive: boolean;
  /** configuredKey 是敏感键的「已配置」派生标记键。 */
  configuredKey?: string;
}

/** connectorSettingsKeys 汇总连接器注册表占用的全部系统设置键，供保存草稿与契约测试共用。 */
export const connectorSettingsKeys = (connectors: ConnectorMeta[]): ConnectorSettingsKey[] => {
  // keys 保存按注册表顺序展开的字段键与能力键。
  const keys: ConnectorSettingsKey[] = [];
  for (const /* connector 是当前遍历的连接器定义。 */ connector of connectors) {
    for (const /* field 是当前连接器的凭据字段。 */ field of connector.fields) {
      keys.push({ key: field.key, sensitive: field.sensitive === true, configuredKey: field.configuredKey });
    }
    for (const /* capability 是当前连接器的能力定义。 */ capability of connector.capabilities) {
      keys.push({ key: capability.toggleKey, sensitive: false });
      if (capability.allowlistKey) keys.push({ key: capability.allowlistKey, sensitive: false });
    }
  }
  return keys;
};
