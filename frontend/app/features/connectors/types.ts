import type { Dispatch,ElementType,SetStateAction } from 'react';
import type { SaveBarState } from '../../../shared/ui/SaveBar';
import type { NotificationChannel,NotificationChannelType,NotificationEventType,SystemSettings } from './api';

// NotificationField 描述通知渠道表单中的一个配置字段。
export interface NotificationField {
  // key 是提交到渠道配置的字段名。
  key: string;
  // label 是字段在表单中的中文名称。
  label: string;
  // placeholder 是输入框的示例提示。
  placeholder?: string;
  // type 是输入控件的 HTML 类型。
  type?: 'text' | 'password' | 'number';
  // sensitive 表示该字段是凭据明文，服务端只回传其是否已保存，编辑时一律不回显。
  sensitive?: boolean;
  // required 表示该字段是否必须填写。
  required?: boolean;
  // help 是字段下方的补充说明。
  help?: string;
}

// NotificationGuide 描述如何获取某类通知渠道凭据。
export interface NotificationGuide {
  // steps 是按顺序展示的配置步骤。
  steps: string[];
  // urlFormat 是可选的 URL 或请求格式示例。
  urlFormat?: string;
  // note 是渠道配置的额外注意事项。
  note?: string;
}

// NotificationChannelMeta 描述通知渠道的静态表单和展示配置。
export interface NotificationChannelMeta {
  // label 是渠道类型的中文名称。
  label: string;
  // icon 是渠道列表和表单中使用的图标组件。
  icon: ElementType;
  // fields 是渠道配置字段定义。
  fields: NotificationField[];
  // credential 是渠道凭据声明：keys 为凭据键，providerConnectorId 为可代填这些键的上游连接器。
  credential: NotificationCredentialSpec;
  // guide 是渠道凭据获取指南。
  guide: NotificationGuide;
}

// NotificationCredentialSpec 描述一种渠道类型把哪些配置键当作凭据，以及可由哪个连接器统一提供。
export interface NotificationCredentialSpec {
  // keys 是该渠道类型的凭据键，只有这些键参与必填豁免与显式清除。
  keys: string[];
  // providerConnectorId 是能代填这些凭据的连接器标识，缺省表示该类型凭据只能由渠道自填。
  providerConnectorId?: string;
}

// CredentialSource 描述当前渠道凭据由哪个已就绪的连接器统一提供。
export interface CredentialSource {
  // hiddenKeys 是由连接器接管、无需在渠道表单展示的凭据键。
  hiddenKeys: string[];
  // connectorName 是提供凭据的连接器中文名称，用于弹窗内的来源说明。
  connectorName?: string;
}

// NotificationEventMeta 描述可供渠道订阅的一类系统事件。
export interface NotificationEventMeta {
  // value 是后端识别的事件类型值。
  value: NotificationEventType;
  // label 是事件的中文名称。
  label: string;
  // description 是事件在绑定组件中的说明。
  description: string;
}

// NotificationForm 描述新建或编辑通知渠道的表单值。
export interface NotificationForm {
  // name 是渠道名称。
  name: string;
  // type 是当前渠道类型。
  type: NotificationChannelType;
  // enabled 表示是否启用当前渠道。
  enabled: boolean;
  // preserveSMTP 表示编辑邮件时保留服务端现有 SMTP，仅单独更新收件地址。
  preserveSMTP?: boolean;
  // credentialAction 表示本次保存对渠道级凭据的意图：clear 显式删除，retain 与缺省都表示沿用服务端原值。
  credentialAction?: 'retain' | 'clear';
  // config 保存渠道类型对应的配置值。
  config: Record<string, unknown>;
  // event_types 保存用户选择的通知事件，空数组表示全部事件。
  event_types: NotificationEventType[];
}

// NotificationPayload 描述保存通知渠道时发送给 API 的请求体。
export interface NotificationPayload {
  // name 是保存后的渠道名称。
  name: string;
  // type 是保存后的渠道类型。
  type: string;
  // config 是显式替换的渠道配置；保留 SMTP 时省略。
  config?: Record<string, unknown>;
  // email_recipient 只修改收件地址，不读取或替换已有发信凭据。
  email_recipient?: string;
  // event_types 是保存后的事件订阅集合。
  event_types: NotificationEventType[];
  // enabled 是保存后的启用状态。
  enabled: boolean;
}

// NotificationToast 描述页面底部的短暂操作提示。
export interface NotificationToast {
  // type 决定提示的成功或失败样式。
  type: 'success' | 'error';
  // text 是提示内容。
  text: string;
}

// ConnectorsState 暴露连接器页面的数据、表单和操作边界。
export interface ConnectorsState {
  // channels 是当前用户可用的通知渠道列表。
  channels: NotificationChannel[];
  // loading 表示渠道列表是否正在加载。
  loading: boolean;
  // showModal 表示渠道编辑弹窗是否打开。
  showModal: boolean;
  // editing 是当前正在编辑的渠道，新增时为空。
  editing: NotificationChannel | null;
  // saving 表示渠道或 SMTP 配置是否正在保存。
  saving: boolean;
  // testingId 是当前正在发送测试通知的渠道 ID。
  testingId: string;
  // toast 是当前待展示的操作提示。
  toast: NotificationToast | null;
  // smtp 是系统级邮件配置。
  smtp: SystemSettings;
  // connectorSettings 是连接器凭据与能力开关的系统配置草稿。
  connectorSettings: SystemSettings;
  // connectorSaving 表示连接器配置是否正在保存。
  connectorSaving: boolean;
  // credentialSource 描述当前渠道类型的凭据是否由已就绪的连接器统一提供，为空表示需要渠道自填或沿用已保存值。
  credentialSource: CredentialSource | null;
  // savedCredentialKeys 是当前编辑渠道已保存的凭据键名，只含键名不含值，供弹窗提示留空沿用。
  savedCredentialKeys: string[];
  // settingsLoading 表示管理员的系统设置（连接器草稿与 SMTP）是否仍在读取。
  settingsLoading: boolean;
  // settingsError 是系统设置读取失败的用户可见文案，非空时对应区块展示错误态与重试入口。
  settingsError: string;
  // connectorSaveState 是连接器区块保存条的四态，由保存请求的进展与结果驱动。
  connectorSaveState: SaveBarState;
  // connectorSaveError 是连接器区块保存失败的具体原因，供保存条失败态展示。
  connectorSaveError: string;
  // smtpSaveState 是 SMTP 区块保存条的四态，与连接器保存互不影响。
  smtpSaveState: SaveBarState;
  // smtpSaveError 是 SMTP 区块保存失败的具体原因，供保存条失败态展示。
  smtpSaveError: string;
  // smtpSaving 表示系统 SMTP 是否正在保存。
  smtpSaving: boolean;
  // showSmtpPassword 控制系统 SMTP 密码明文展示。
  showSmtpPassword: boolean;
  // showChannelSmtpPassword 控制独立 SMTP 密码明文展示。
  showChannelSmtpPassword: boolean;
  // form 是当前渠道编辑表单值。
  form: NotificationForm;
  // setForm 更新渠道编辑表单。
  setForm: Dispatch<SetStateAction<NotificationForm>>;
  // setSmtp 更新系统 SMTP 配置表单。
  setSmtp: Dispatch<SetStateAction<SystemSettings>>;
  // setConnectorSettings 更新连接器配置草稿。
  setConnectorSettings: Dispatch<SetStateAction<SystemSettings>>;
  // setShowSmtpPassword 更新系统 SMTP 密码展示状态。
  setShowSmtpPassword: Dispatch<SetStateAction<boolean>>;
  // setShowChannelSmtpPassword 更新独立 SMTP 密码展示状态。
  setShowChannelSmtpPassword: Dispatch<SetStateAction<boolean>>;
  // loadChannels 刷新通知渠道列表。
  loadChannels: () => Promise<void>;
  // loadSmtp 重新读取管理员可见的系统设置（连接器草稿与 SMTP）。
  loadSmtp: () => Promise<void>;
  // refresh 是页头「刷新」的语义：管理员同时重载渠道与系统设置，普通用户只刷新渠道。
  refresh: () => Promise<void>;
  // openCreate 打开新建渠道弹窗。
  openCreate: () => void;
  // openEdit 打开已有渠道编辑弹窗。
  openEdit: (channel: NotificationChannel) => Promise<void>;
  // closeModal 关闭弹窗并取消当前保存请求。
  closeModal: () => void;
  // resetCredentialState 清空已保存凭据键与清除意图，渠道类型切换后不再沿用上一类型的凭据状态。
  resetCredentialState: () => void;
  // showToast 展示短暂的成功或失败提示。
  showToast: (type: NotificationToast['type'], text: string) => void;
  // handleSave 保存当前渠道配置。
  handleSave: () => Promise<void>;
  // handleDelete 删除指定通知渠道。
  handleDelete: (channel: NotificationChannel) => Promise<void>;
  // handleToggleEnabled 切换指定渠道的启用状态。
  handleToggleEnabled: (channel: NotificationChannel) => Promise<void>;
  // handleTest 发送指定渠道的测试通知。
  handleTest: (channel: NotificationChannel) => Promise<void>;
  // handleSaveSmtp 保存系统级 SMTP 配置。
  handleSaveSmtp: () => Promise<void>;
  // handleSaveConnector 保存连接器凭据与能力开关。
  handleSaveConnector: () => Promise<void>;
  // resetConnectorSaveState 把连接器保存条复位为空闲待提交。
  resetConnectorSaveState: () => void;
  // resetSmtpSaveState 把 SMTP 保存条复位为空闲待提交。
  resetSmtpSaveState: () => void;
}
