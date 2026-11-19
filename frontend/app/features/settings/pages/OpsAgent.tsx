import { Radio,RefreshCw,ShieldCheck } from 'lucide-react';
import React,{ useCallback,useState } from 'react';
import { DependencyRow } from '../../../../shared/ui/DependencyRow';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { PageShell } from '../../../../shared/ui/PageShell';
import { SaveBar } from '../../../../shared/ui/SaveBar';
import type { SaveBarState } from '../../../../shared/ui/SaveBar';
import { SectionHeader } from '../../../../shared/ui/SectionHeader';
import { useSettings } from '../hooks';
import { isQQBotConnectorReady } from '../api';
import { OpsConversationPanel } from '../components/OpsConversationPanel';

/** OpsAgentProps 描述运营 Agent 页可选的跨页跳转能力。 */
export interface OpsAgentProps {
  /** onNavigate 由外层提供时，凭据依赖行渲染可点击的「前往连接器页」入口；缺省时只展示就绪状态。 */
  onNavigate?: (tab: string) => void;
}

/** OpsAgent 管理 QQ 入站命令开关与发送者白名单，并提供运营会话列表、消息与 Web 人工发送。 */
const OpsAgent: React.FC<OpsAgentProps> = ({ onNavigate }) => {
  // featureState 是 Settings Hook 提供的状态与动作集合，scope=ops 只保存 QQ 入站两个字段。
  const {
    settings, loading, loadError, saveError,
    loadSettings, handleSave, setSettings,
  } = useSettings('ops');

  // saveBarState 是区块级保存条的四态，由一次保存的结果驱动，成功提示展示完毕后回落为空闲。
  const [saveBarState, setSaveBarState] = useState<SaveBarState>('idle');

  // handlePageSave 提交运营配置草稿，并把返回值映射为保存条的成功或失败态。
  const handlePageSave = useCallback(/* 当前回调把保存请求的进展映射为保存条状态。 */ async () => {
    setSaveBarState('saving');
    // saved 是本次保存是否成功；失败时保存条常驻并提供重试入口。
    const saved = await handleSave({ silentSuccess: true });
    setSaveBarState(saved ? 'success' : 'error');
  }, [handleSave]);

  // settleSaveBar 在成功提示展示满 2 秒后把保存条复位为空闲待提交。
  const settleSaveBar = useCallback(/* 当前回调在成功提示超时后把保存条复位为空闲。 */ () => setSaveBarState('idle'), []);

  if (!settings) {
    return (
      <div className="p-8 text-center text-gray-400 space-y-3">
        {loadError || (loading ? '加载配置中...' : '暂无配置')}
        {!loading && loadError && (
          <div>
            <button type="button" className="ios-btn-primary px-4 py-2 rounded-xl" onClick={loadSettings}>重新加载</button>
          </div>
        )}
      </div>
    );
  }

  // commandsEnabled 表示 QQ 入站命令开关是否已打开；未配置按关闭处理。
  const commandsEnabled = settings['qqbot.commands_enabled'] === true;
  // openidAllowlist 是命令发送者白名单的草稿原文，支持换行或逗号分隔。
  const openidAllowlist = String(settings['qqbot.command_openids'] ?? '');
  // credentialsReady 表示 QQ 机器人凭据是否就绪，决定对话区能否发送人工消息。
  const credentialsReady = isQQBotConnectorReady(settings);

  return (
    <PageShell>
      <PageHeader
        icon={Radio}
        title="运营 Agent"
        subtitle="走 QQ 通道；开关与白名单在此，凭据在连接器"
        actions={(
          <button
            type="button"
            onClick={loadSettings}
            className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            刷新
          </button>
        )}
      />

      {/* 设置区：只保存入站命令开关与白名单，QQ 凭据仍由连接器页唯一维护。 */}
      <section className="space-y-4">
        <SectionHeader
          icon={ShieldCheck}
          tone="violet"
          title="QQ 入站命令"
          description="开启后服务会连接 QQ 网关，只接收与回复白名单内发送者的只读指令"
        />
        <div className="ios-card rounded-xl p-6 bg-white space-y-5">
          <label className="flex items-start gap-3 rounded-xl border border-brand-200 bg-brand-50 p-4 cursor-pointer">
            <input
              type="checkbox"
              className="mt-1"
              aria-label="启用运营 Agent（QQ 入站）"
              checked={commandsEnabled}
              onChange={/* 勾选变化更新入站命令开关草稿。 */ event => setSettings({ ...settings, 'qqbot.commands_enabled': event.target.checked })}
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
              onChange={/* 白名单输入变化更新草稿原文。 */ event => setSettings({ ...settings, 'qqbot.command_openids': event.target.value })}
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
            state={saveBarState}
            onSave={/* 提交入站命令开关与白名单。 */ () => void handlePageSave()}
            label="保存运营配置"
            scopeLabel="保存运营 Agent 配置"
            errorMessage={/* 使用 Hook 捕获的具体失败原因，未取到时回落到保存条默认文案。 */ saveError || undefined}
            successMessage="运营 Agent 配置已保存"
            sticky={false}
            onSettled={settleSaveBar}
          />
        </div>
      </section>

      {/* 对话区：即时操作，不参与上面的草稿保存；凭据未就绪时输入禁用并给出配置指引。 */}
      <section className="space-y-4">
        <SectionHeader
          icon={Radio}
          tone="violet"
          title="QQ 会话"
          description="手动刷新，不轮询；发送成功后才落库，失败会保留输入原文"
        />
        <OpsConversationPanel credentialsReady={credentialsReady} />
      </section>
    </PageShell>
  );
};

export default OpsAgent;
