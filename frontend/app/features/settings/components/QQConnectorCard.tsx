import React from 'react';
import { Bot, ExternalLink, Eye, EyeOff, ShieldCheck } from 'lucide-react';
import { useState } from 'react';
import type { SystemSettings } from '../api';

/** QQ_OPEN_PLATFORM_URL 是管理员创建机器人并获取 AppID 与 AppSecret 的 QQ 开放平台入口。 */
export const QQ_OPEN_PLATFORM_URL = 'https://q.qq.com/';

/** QQ_CONNECTOR_GUIDE_STEPS 按顺序列出在 QQ 开放平台创建官方机器人并取回凭据的步骤。 */
export const QQ_CONNECTOR_GUIDE_STEPS: string[] = [
  '打开 QQ 开放平台 q.qq.com，用手机 QQ 扫码登录（官方机器人通道，无需第三方协议端）',
  '进入「机器人」→ 创建机器人，填写名称、简介与头像后提交',
  '在机器人的「开发设置」页复制 AppID 与 AppSecret（AppSecret 仅展示一次，请立即保存）',
  '回到本卡片填入 AppID 与 AppSecret 并保存，再到「通知设置」新建 QQ 机器人渠道即可发送',
];

// QQConnectorCardProps 描述 QQ 连接器配置卡的受控输入。
interface QQConnectorCardProps {
  /** settings 是当前系统配置草稿，读取 qqbot.app_id 与 qqbot.app_secret 的配置状态。 */
  settings: SystemSettings;
  /** onChange 在 AppID 或 AppSecret 输入变化后写回系统配置草稿补丁。 */
  onChange: (patch: Partial<SystemSettings>) => void;
}

// QQConnectorCard 渲染连接器区的「QQ 机器人」配置卡：引导前往开放平台取凭据并保存为系统级连接器。
export const QQConnectorCard: React.FC<QQConnectorCardProps> = ({ settings, onChange }) => {
  // showSecret 控制 AppSecret 输入框是否明文显示。
  const [showSecret, setShowSecret] = useState(false);
  // appID 是当前草稿中的机器人 AppID，未配置时为空串。
  const appID = String(settings['qqbot.app_id'] ?? '');
  // secretDraft 是当前草稿中的 AppSecret，服务端不回显明文，未输入时为空串。
  const secretDraft = String(settings['qqbot.app_secret'] ?? '');
  // secretConfigured 表示服务端是否已保存可用的 AppSecret。
  const secretConfigured = settings['qqbot.app_secret_configured'] === true;
  // ready 表示当前是否已具备完整凭据：AppID 非空且 AppSecret 已配置或本次新填。
  const ready = appID.trim() !== '' && (secretConfigured || secretDraft.trim() !== '');
  // commandsEnabled 表示是否开启入站命令，控制白名单输入的可见性。
  const commandsEnabled = settings['qqbot.commands_enabled'] === true;
  // commandOpenIDs 是命令发送者白名单草稿。
  const commandOpenIDs = String(settings['qqbot.command_openids'] ?? '');

  return (
    <div className="ios-card rounded-xl p-6 bg-white space-y-5">
      <div className="flex items-start gap-2">
        <div className="p-1.5 rounded-lg bg-sky-500 text-white mt-0.5">
          <Bot className="w-4 h-4" />
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <h4 className="text-sm font-bold text-gray-800">QQ 机器人连接器</h4>
            <span
              data-testid="qq-connector-status"
              className={`text-[11px] font-bold px-2 py-0.5 rounded-full ${ready ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-500'}`}
            >
              {ready ? '凭据已就绪' : '未配置'}
            </span>
          </div>
          <p className="text-xs text-gray-500 mt-0.5">在系统级配置一次 AppID / AppSecret，通知渠道里新建「QQ 机器人」即可直接复用，无需逐条重复填写。</p>
        </div>
      </div>

      <div className="rounded-xl border border-sky-100 bg-sky-50 p-4 space-y-2">
        <p className="text-xs font-bold text-sky-900">如何获取 AppID 与 AppSecret</p>
        <ol className="text-xs leading-5 text-sky-900 list-decimal pl-4 space-y-1">
          {QQ_CONNECTOR_GUIDE_STEPS.map(/* step 是当前引导步骤文案，index 用于生成稳定 key。 */ (step, index) => (
            <li key={index}>{step}</li>
          ))}
        </ol>
        <a
          href={QQ_OPEN_PLATFORM_URL}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 text-xs font-bold text-sky-700 hover:text-sky-900 underline"
        >
          <ExternalLink className="w-3.5 h-3.5" />
          前往 QQ 开放平台
        </a>
      </div>

      <div className="space-y-2">
        <label className="block text-sm font-bold text-gray-800" htmlFor="qq-connector-app-id">机器人 AppID</label>
        <input
          id="qq-connector-app-id"
          aria-label="QQ 机器人 AppID"
          type="text"
          value={appID}
          onChange={/* AppID 输入变化后写回系统配置草稿。 */ event => onChange({ 'qqbot.app_id': event.target.value })}
          placeholder="例如：1020xxxxx"
          autoComplete="off"
          className="w-full ios-input px-4 py-3 rounded-xl font-mono text-sm"
        />
      </div>

      <div className="space-y-2">
        <label className="block text-sm font-bold text-gray-800" htmlFor="qq-connector-app-secret">机器人 AppSecret</label>
        <div className="relative">
          <input
            id="qq-connector-app-secret"
            aria-label="QQ 机器人 AppSecret"
            type={showSecret ? 'text' : 'password'}
            value={secretDraft}
            onChange={/* AppSecret 输入变化后写回系统配置草稿。 */ event => onChange({ 'qqbot.app_secret': event.target.value })}
            className="w-full ios-input px-4 py-3 pr-12 rounded-xl font-mono text-sm"
            autoComplete="off"
            placeholder={secretConfigured ? '已配置，如需替换请输入新 AppSecret' : '请输入 QQ 机器人 AppSecret'}
          />
          <button
            type="button"
            onClick={/* 切换 AppSecret 明文显示状态。 */ () => setShowSecret(!showSecret)}
            className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600"
            title={showSecret ? '隐藏 AppSecret' : '显示 AppSecret'}
          >
            {showSecret ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
          </button>
        </div>
        {secretConfigured && (
          <p className="text-xs text-gray-500">服务端已保存 AppSecret 且仅本地加密存储，页面不会回显明文；留空保存表示保持原值。</p>
        )}
      </div>

      <div className="flex items-start gap-2 rounded-xl bg-amber-50 px-3 py-2.5">
        <ShieldCheck className="w-4 h-4 mt-0.5 text-amber-600 flex-shrink-0" />
        <p className="text-xs leading-5 text-amber-800">
          官方机器人通道的主动推送有额度限制：同一用户或同一群每月仅 4 条主动消息，被动回复窗口为单聊 60 分钟、群聊 5 分钟。
          机器人的 openid 与 AppID 绑定，换机器人后通知渠道里的 openid 需重新获取。
        </p>
      </div>

      <label className="flex items-start gap-3 rounded-xl border border-sky-200 bg-sky-50 p-4 cursor-pointer" htmlFor="qq-connector-commands-enabled">
        <input
          id="qq-connector-commands-enabled"
          type="checkbox"
          aria-label="启用 QQ 入站命令"
          className="mt-1"
          checked={commandsEnabled}
          onChange={/* 入站命令开关变化后写回系统配置草稿。 */ event => onChange({ 'qqbot.commands_enabled': event.target.checked })}
        />
        <span>
          <span className="block text-sm font-bold text-sky-900">启用入站命令</span>
          <span className="mt-1 block text-xs leading-5 text-sky-800">
            开启后服务会连接 QQ 网关，接收「销量 / 健康 / 会话 / 帮助」四类只读命令并回复结果。关闭时不产生任何对外连接。
          </span>
        </span>
      </label>

      <p className="text-xs text-gray-500">
        凭据与开关在下次服务重启后生效；网关断开后服务会按指数退避自动重连，无需手动干预。
      </p>

      {commandsEnabled && (
        <div className="space-y-2">
          <label className="block text-sm font-bold text-gray-800" htmlFor="qq-connector-command-openids">命令发送者白名单（openid）</label>
          <textarea
            id="qq-connector-command-openids"
            aria-label="QQ 命令发送者白名单"
            value={commandOpenIDs}
            onChange={/* 白名单输入变化后写回系统配置草稿。 */ event => onChange({ 'qqbot.command_openids': event.target.value })}
            placeholder={'留空表示拒绝所有发送者。每行一个或用逗号分隔，例如：\n0A1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P'}
            className="w-full ios-input px-4 py-3 rounded-xl h-24 text-sm leading-6 font-mono resize-y"
          />
          <p className="text-xs text-gray-500">
            机器人一旦上线，任何找到它的人都能发消息；只有白名单内的 openid 才能触发命令。未授权者会收到含其 openid 的提示，把该标识粘贴到这里即可开通。
          </p>
        </div>
      )}
    </div>
  );
};
