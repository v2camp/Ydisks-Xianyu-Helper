import { Bot,PlugZap,Radio,RefreshCw } from 'lucide-react';
import React,{ useCallback } from 'react';
import { AsyncSection } from '../../../../shared/ui/AsyncSection';
import { DependencyRow } from '../../../../shared/ui/DependencyRow';
import type { DependencyState } from '../../../../shared/ui/DependencyRow';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { PageShell } from '../../../../shared/ui/PageShell';
import { SaveBar } from '../../../../shared/ui/SaveBar';
import type { SaveBarState } from '../../../../shared/ui/SaveBar';
import { SectionHeader } from '../../../../shared/ui/SectionHeader';
import { useAgentCenter } from '../agentCenterHooks';
import { MCPServiceCard } from '../components/MCPServiceCard';

/** AgentCenterProps 描述 Agent 中心页可选的跨页跳转能力。 */
export interface AgentCenterProps {
  /** onNavigate 由外层提供时，依赖行渲染可点击的跳转入口；未提供时只展示依赖状态，页面自身不依赖路由。 */
  onNavigate?: (tab: string) => void;
}

/** dependencyState 把尚未到达的依赖态统一显示为「读取中」，绝不先闪出「未配置」。 */
const dependencyState = (state: DependencyState | undefined): DependencyState => state ?? 'loading';

/** AgentCenter 按「谁决定调用能力」归拢三类 Agent 的入口，并直接配置租户级客服 Agent 的默认开关与档位。 */
const AgentCenter: React.FC<AgentCenterProps> = ({ onNavigate }) => {
  // center 是本页的配置草稿、保存状态与依赖状态。
  const {
    settings, loading, loadError, saving, saveMessage,
    dependencies, dependenciesLoading, dependenciesError,
    load, reloadDependencies, updateDraft, submit, resetSaveMessage,
  } = useAgentCenter();

  // refreshing 表示页头刷新是否进行中：租户配置与依赖状态任一在读取时都禁用按钮，避免重复点击叠加并发请求。
  const refreshing = loading || dependenciesLoading;

  // refreshAll 同时刷新本页两块独立数据：租户配置与前置依赖；只刷配置会让依赖行（AI 模型/接管范围/外部工具/QQ 连接器）停在旧状态。
  const refreshAll = useCallback(/* 当前回调把页头「刷新」收敛为一次配置读取加一次依赖读取。 */ () => {
    load();
    reloadDependencies();
  }, [load, reloadDependencies]);

  // saveBarState 是区块级保存条的四态：保存中优先，其次沿用最近一次提交的成功或失败结果，其余为空闲待提交。
  const saveBarState: SaveBarState = saving
    ? 'saving'
    : saveMessage?.type === 'error'
      ? 'error'
      : saveMessage?.type === 'success'
        ? 'success'
        : 'idle';

  // settleSaveBar 在成功提示停留结束后清空结果，让保存条回到待提交态而不是长期停留在绿色条。
  const settleSaveBar = useCallback(/* 当前回调把保存条从成功态复位为空闲。 */ () => resetSaveMessage(), [resetSaveMessage]);

  return (
    <PageShell>
      <PageHeader
        icon={Bot}
        title="Agent 中心"
        subtitle="三类 Agent 的入口与前置依赖；能力本体在 AI 设置"
        actions={(
          <button
            type="button"
            onClick={refreshAll}
            disabled={refreshing}
            className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors disabled:opacity-40"
          >
            <RefreshCw className={`w-4 h-4 ${refreshing ? 'animate-spin' : ''}`} />
            刷新
          </button>
        )}
      />

      {/* 管理端 Agent：外部 Harness 由人在场操作，凭管理员身份直接调用工具，权限与工具范围都不设档位。 */}
      <section className="space-y-4">
        <SectionHeader
          icon={PlugZap}
          tone="indigo"
          title="对外 MCP 服务（入站）"
          description="把本服务能力以 MCP 协议开放给外部 Harness；与 AI 设置页的外部 MCP 工具（出站）方向相反。"
        />
        {/* MCP 服务卡：启停、接入地址、Harness 配置示例、令牌生命周期与调用审计，操作即时保存，不随本页草稿提交。
            启用状态与接入地址只在这里展示一份，页面上不再另设摘要卡，避免同一事实两处渲染后失步。 */}
        <MCPServiceCard />
      </section>

      {/* 客服端 Agent：无人监督、作用域锁定单个账号，因此能力由档位统一约束，不逐个工具放行。 */}
      <section className="space-y-4">
        <SectionHeader
          icon={Bot}
          tone="brand"
          title="客服端 Agent"
          description="无人监督、作用域锁定单个账号；能力由档位统一约束"
        />
        <div className="ios-card rounded-xl p-6 bg-white space-y-5">
          {!settings ? (
            <div className="p-6 text-center text-gray-400 space-y-3">
              {loadError || (loading ? '加载配置中...' : '暂无配置')}
              {!loading && loadError && (
                <div>
                  <button type="button" className="ios-btn-primary px-4 py-2 rounded-xl" onClick={load}>重新加载</button>
                </div>
              )}
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between p-4 bg-brand-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900">启用客服 Agent</div>
                  <div className="text-xs text-gray-500">开启后客服 Agent 可自主查询并答复买家；未开启时卖家账号只走既有的规则与 AI 回复通道</div>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={settings.enabled}
                  aria-label="启用客服 Agent"
                  onClick={/* 切换租户默认开关草稿。 */ () => updateDraft({ enabled: !settings.enabled })}
                  className={`w-14 h-8 rounded-full transition-colors duration-300 relative flex-shrink-0 ${settings.enabled ? 'bg-brand' : 'bg-gray-300'}`}
                >
                  <span className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${settings.enabled ? 'translate-x-6' : 'translate-x-0'}`} />
                </button>
              </div>

              <div className="space-y-3">
                <div className="text-sm font-bold text-gray-800">能力档位</div>
                <div className="grid gap-3 md:grid-cols-3">
                  {settings.presets.map(/* option 是服务端下发的当前档位选项。 */ option => (
                    <button
                      key={option.value}
                      type="button"
                      aria-pressed={settings.preset === option.value}
                      onClick={/* 选中当前档位并写回草稿。 */ () => updateDraft({ preset: option.value })}
                      className={`rounded-xl border p-4 text-left transition-colors ${settings.preset === option.value ? 'border-brand-400 bg-brand-50' : 'border-gray-200 hover:border-gray-300'}`}
                    >
                      <span className="block text-sm font-bold text-gray-900">{option.label}</span>
                      <span className="mt-1 block text-xs leading-5 text-gray-600">{option.description}</span>
                    </button>
                  ))}
                </div>
                {settings.presets.length === 0 && (
                  <p className="text-xs text-warning-700">服务端未下发档位名单，暂时无法切换档位，请确认服务端版本与本页面匹配。</p>
                )}
                <p className="text-xs leading-5 text-gray-500">
                  这里设置的是租户默认档位。单个卖家账号可在账号列表里单独覆盖，未覆盖的账号按此默认生效。
                  档位与能力内核一一对应，界面不另外维护一份工具清单。
                </p>
              </div>

              {/* 依赖区：客服 Agent 依赖的三项能力只读展示，配置动作统一跳转到 AI 设置页，避免本页出现重复编辑入口。 */}
              <div className="space-y-2">
                <div className="text-sm font-bold text-gray-800">前置依赖</div>
                <AsyncSection
                  loading={dependenciesLoading}
                  error={dependenciesError}
                  onRetry={reloadDependencies}
                  retryLabel="重新读取"
                  className="space-y-2"
                >
                  <DependencyRow
                    testId="dependency-ai-model"
                    label="AI 模型"
                    state={dependencyState(dependencies?.aiModel)}
                    detail="模型名称与 API 密钥都在 AI 设置页维护"
                    linkLabel="前往 AI 设置"
                    onNavigate={/* 跳转到 AI 设置页补全模型或密钥。 */ () => onNavigate?.('ai-settings')}
                  />
                  <DependencyRow
                    testId="dependency-ai-scope"
                    label="AI 接管范围"
                    state={dependencyState(dependencies?.aiScope)}
                    detail="留空时沿用内置接管边界"
                    linkLabel="前往 AI 设置"
                    onNavigate={/* 跳转到 AI 设置页调整接管范围。 */ () => onNavigate?.('ai-settings')}
                  />
                  <DependencyRow
                    testId="dependency-ai-tools"
                    label="外部 MCP 工具"
                    state={dependencyState(dependencies?.aiTools)}
                    detail="外部 MCP 工具服务（出站）在 AI 设置页添加"
                    linkLabel="前往 AI 设置"
                    onNavigate={/* 跳转到 AI 设置页添加外部 MCP 工具服务。 */ () => onNavigate?.('ai-settings')}
                  />
                </AsyncSection>
              </div>

              <SaveBar
                state={saveBarState}
                onSave={/* 提交租户默认开关与档位。 */ () => void submit()}
                label="保存"
                scopeLabel="保存客服 Agent 配置"
                successMessage={saveMessage?.type === 'success' ? saveMessage.text : '客服 Agent 配置已保存'}
                errorMessage={saveMessage?.type === 'error' ? saveMessage.text : '保存客服 Agent 配置失败'}
                sticky={false}
                onSettled={settleSaveBar}
              />
            </>
          )}
        </div>
      </section>

      {/* 运营 Agent：启用前提是 QQ 机器人凭据已配置，配置项就地留在「连接器」页的 QQ 机器人卡片，本页只作指引。 */}
      <section className="space-y-4">
        <SectionHeader
          icon={Radio}
          tone="violet"
          title="运营 Agent"
          description="走 QQ 通道，由管理员在 QQ 里下发指令"
        />
        <div className="ios-card rounded-xl p-6 bg-white space-y-3">
          <p className="text-sm leading-6 text-gray-700">
            运营 Agent 走 QQ 通道，由管理员在 QQ 里直接下发指令，它的启用前提是 QQ 机器人凭据已经配置好。
            因此开关与命令发送者白名单留在「连接器」页的 QQ 机器人连接器卡片里，不迁到本页。
          </p>
          <p className="text-xs leading-5 text-gray-500">
            放在一起会出现「在 Agent 中心打开了、却因为 QQ 没配好而毫无反应」的困惑。
            本页不对该 Agent 提供配置项。
          </p>
          <DependencyRow
            testId="dependency-qq-connector"
            label="QQ 机器人连接器"
            state={dependencyState(dependencies?.qqConnector)}
            detail="AppID 与 AppSecret 在连接器页维护"
            linkLabel="前往连接器页"
            onNavigate={/* 跳转到连接器页补全 QQ 机器人凭据。 */ () => onNavigate?.('connectors')}
          />
        </div>
      </section>
    </PageShell>
  );
};

export default AgentCenter;
