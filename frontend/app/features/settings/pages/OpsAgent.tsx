import { Radio,RefreshCw,ShieldCheck } from 'lucide-react';
import React from 'react';
import { DependencyRow } from '../../../../shared/ui/DependencyRow';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { PageShell } from '../../../../shared/ui/PageShell';
import { SaveBar } from '../../../../shared/ui/SaveBar';
import { SectionHeader } from '../../../../shared/ui/SectionHeader';
import { isQQConnectorReady } from '../api';
import { useUserIntegrations } from '../../../../shared/async/useUserIntegrations';
import { OpsConversationPanel } from '../components/OpsConversationPanel';
import { SettingsDisclosure } from '../components/SettingsDisclosure';

/** OpsAgentProps 描述运营 Agent 页可选的跨页跳转能力。 */
export interface OpsAgentProps {
  /** onNavigate 由外层提供时，凭据依赖行渲染可点击的「前往连接器页」入口；缺省时只展示就绪状态。 */
  onNavigate?: (tab: string) => void;
}

/** OpsAgent 管理当前用户的 QQ 入站命令开关与发送者白名单，并提供运营会话列表、消息与 Web 人工发送。 */
const OpsAgent: React.FC<OpsAgentProps> = ({ onNavigate }) => {
  // integrations 提供当前用户的 QQ、AI 与 SMTP 集成配置；本页只编辑 QQ 入站两项。
  const integrations = useUserIntegrations();
  // settings 是当前用户的集成配置草稿。
  const settings = integrations.settings;

  // commandsEnabled 表示 QQ 入站命令开关是否已打开；未配置按关闭处理。
  const commandsEnabled = settings['qqbot.commands_enabled'] === true;
  // openidAllowlist 是命令发送者白名单的草稿原文，支持换行或逗号分隔。
  const openidAllowlist = String(settings['qqbot.command_openids'] ?? '');
  // handlePageSave 只提交入站命令开关与白名单两个键，避免把其他集成配置一并回写。
  const handlePageSave = () => void integrations.save({
    'qqbot.commands_enabled': commandsEnabled,
    'qqbot.command_openids': openidAllowlist,
  });
  // credentialsReady 表示 QQ 机器人凭据是否就绪，决定对话区能否发送人工消息。
  const credentialsReady = isQQConnectorReady(settings);
  // applyIntegrationPatch 把单个集成配置键值合并进当前用户草稿。
  const applyIntegrationPatch = (key: string, value: string | boolean) => {
    integrations.setSettings(/* previous 是当前集成配置草稿。 */ previous => ({ ...previous, [key]: value }));
  };
  // handleCommandsEnabledChange 更新 QQ 入站命令开关草稿。
  const handleCommandsEnabledChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    applyIntegrationPatch('qqbot.commands_enabled', event.target.checked);
  };
  // handleAllowlistChange 更新命令发送者白名单草稿原文。
  const handleAllowlistChange = (event: React.ChangeEvent<HTMLTextAreaElement>) => {
    applyIntegrationPatch('qqbot.command_openids', event.target.value);
  };

  if (integrations.loading && Object.keys(settings).length === 0) {
    return (
      <div className="p-8 text-center text-gray-400 space-y-3">
        {integrations.loadError || '加载配置中...'}
      </div>
    );
  }

  return (
    <PageShell>
      <PageHeader
        icon={Radio}
        title="运营 Agent"
        subtitle="走 QQ 通道；开关与白名单在此，凭据在连接器"
        actions={(
          <button
            type="button"
            onClick={/* 点击重新读取当前用户的集成配置。 */ () => void integrations.load()}
            className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            刷新
          </button>
        )}
      />

      {/* 会话区是页面主体：左会话列表、右消息与人工发送，优先占据第一屏。 */}
      <section className="space-y-4">
        <SectionHeader
          icon={Radio}
          tone="violet"
          title="QQ 会话"
          description="手动刷新，不轮询；发送成功后才落库，失败会保留输入原文"
        />
        <OpsConversationPanel credentialsReady={credentialsReady} />
      </section>

      {/* 设置区是附属配件：默认收起，展开后才占用空间。 */}
      <SettingsDisclosure
        icon={ShieldCheck}
        tone="violet"
        title="QQ 入站命令"
        description="开启后服务会连接 QQ 网关，只接收与回复白名单内发送者的只读指令"
      >
        <div className="space-y-5">
          <label className="flex items-start gap-3 rounded-xl border border-brand-200 bg-brand-50 p-4 cursor-pointer">
            <input
              type="checkbox"
              className="mt-1"
              aria-label="启用运营 Agent（QQ 入站）"
              checked={commandsEnabled}
              onChange={handleCommandsEnabledChange}
            />
            <span>
              <span className="block text-sm font-bold text-brand-900">启用运营 Agent（QQ 入站）</span>
              <span className="mt-1 block text-xs leading-5 text-brand-800">
                关闭时不产生任何对外连接；开启前提是连接器页的 QQ 机器人凭据已配置完成。
              </span>
            </span>
          </label>

          <div className="space-y-3">
            <label className="block text-sm font-bold text-gray-800" htmlFor="ops-command-openids">命令发送者白名单（openid）</label>
            <textarea
              id="ops-command-openids"
              aria-label="命令发送者白名单（openid）"
              value={openidAllowlist}
              onChange={handleAllowlistChange}
              placeholder={'留空表示拒绝所有发送者。每行一个或用逗号分隔，例如：\n0A1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P'}
              className="h-28 w-full ios-input px-4 py-3 rounded-xl text-sm leading-6 font-mono resize-y"
            />
            <p className="text-xs text-gray-500">
              机器人一旦上线，任何找到它的人都能发消息；只有白名单内的 openid 才能触发命令。未授权者会收到含其 openid 的提示，把该标识粘贴到这里即可开通。
            </p>
          </div>

          {/* 凭据依赖行只读展示，配置动作统一跳到连接器页，避免本页出现重复的凭据编辑入口。 */}
          <DependencyRow
            testId="dependency-qq-connector"
            label="QQ 机器人凭据"
            state={credentialsReady ? 'ready' : 'missing'}
            detail="AppID 与 AppSecret 在连接器页维护"
            linkLabel="前往连接器页"
            onNavigate={/* 跳转到连接器页补全 QQ 机器人凭据。 */ () => onNavigate?.('connectors')}
          />

          <SaveBar
            state={integrations.saveState}
            onSave={/* 提交入站命令开关与白名单。 */ () => handlePageSave()}
            label="保存运营配置"
            scopeLabel="保存运营 Agent 配置"
            errorMessage={/* 使用 Hook 捕获的具体失败原因，未取到时回落到保存条默认文案。 */ integrations.saveError || undefined}
            successMessage="运营 Agent 配置已保存"
            sticky={false}
            onSettled={integrations.resetSaveState}
          />
        </div>
      </SettingsDisclosure>
    </PageShell>
  );
};

export default OpsAgent;
