import {
  Bot,
  RefreshCw,
  Sparkles,
  Zap
} from 'lucide-react';
import React, { useCallback,useState } from 'react';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { PageShell } from '../../../../shared/ui/PageShell';
import { SaveBar } from '../../../../shared/ui/SaveBar';
import type { SaveBarState } from '../../../../shared/ui/SaveBar';
import { SectionHeader } from '../../../../shared/ui/SectionHeader';
import { useSettings } from '../hooks';
import { useCustomerAgent } from '../customerAgentHooks';
import { AIConfigEditor } from '../components/AIConfigEditor';
import { AgentTakeoverHistory } from '../components/AgentTakeoverHistory';
import { MCPServerList } from '../components/MCPServerList';
import { SettingsDisclosure } from '../components/SettingsDisclosure';

/** CustomerAgentProps 描述客服 Agent 页可选的跨页跳转能力。 */
export interface CustomerAgentProps {
  /** onNavigate 由外层提供时，模型与凭据依赖行可跳转到连接器页。 */
  onNavigate?: (tab: string) => void;
}

// CustomerAgent 汇总客服 Agent 的 AI 总闸、接管范围、出站 MCP 工具、能力档位与接管历史；
// 页级保存只提交 AI 行为字段，能力档位走自己的区块级保存条。模型与密钥在连接器页维护。
const CustomerAgent: React.FC<CustomerAgentProps> = ({ onNavigate }) => {
  // featureState 是 Settings Hook 提供的状态与动作集合，scope=ai 只保存 AI 行为字段。
  const {
    settings, loading, loadError, saveError,
    loadSettings, handleSave, setSettings,
  } = useSettings('ai');

  // agent 是能力档位区块的草稿与保存状态；是否运行仍由 AI 总闸统一决定。
  const {
    settings: agentSettings, loading: agentLoading, loadError: agentLoadError,
    saving: agentSaving, saveMessage: agentSaveMessage,
    load: loadAgentSettings, updateDraft: updateAgentDraft, submit: submitAgentSettings, resetSaveMessage: resetAgentSaveMessage,
  } = useCustomerAgent();

  // saveBarState 是页级保存条的四态，由一次保存的结果驱动，成功提示展示完毕后回落为空闲。
  const [saveBarState, setSaveBarState] = useState<SaveBarState>('idle');

  // handlePageSave 触发 AI 白名单配置保存，并把返回值映射为保存条的成功或失败态。
  const handlePageSave = useCallback(/* 当前回调把保存请求的进展映射为保存条状态。 */ async () => {
    setSaveBarState('saving');
    // saved 是本次保存是否成功；失败时保存条常驻并提供重试入口。
    const saved = await handleSave({ silentSuccess: true });
    setSaveBarState(saved ? 'success' : 'error');
  }, [handleSave]);

  // settleSaveBar 在成功提示展示满 2 秒后把保存条复位为空闲待提交。
  const settleSaveBar = useCallback(/* 当前回调在成功提示超时后把保存条复位为空闲。 */ () => setSaveBarState('idle'), []);

  // refreshAll 同时刷新 AI 配置与能力档位；两块数据来源不同，只刷一块会让页面停在半新半旧的状态。
  const refreshAll = useCallback(/* 当前回调把页头刷新收敛为两次独立读取。 */ () => {
    loadSettings();
    loadAgentSettings();
  }, [loadAgentSettings, loadSettings]);

  // agentSaveBarState 是能力档位区块保存条的四态：保存中优先，其次沿用最近一次提交结果。
  const agentSaveBarState: SaveBarState = agentSaving
    ? 'saving'
    : agentSaveMessage?.type === 'error'
      ? 'error'
      : agentSaveMessage?.type === 'success'
        ? 'success'
        : 'idle';

  // settleAgentSaveBar 在成功提示停留结束后清空结果，让区块保存条回到待提交态。
  const settleAgentSaveBar = useCallback(/* 当前回调把能力档位保存条从成功态复位为空闲。 */ () => resetAgentSaveMessage(), [resetAgentSaveMessage]);

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

  return (
    <PageShell>
      <PageHeader
        icon={Bot}
        title="客服 Agent"
        subtitle="AI 引擎与接管范围；被规则通道与运营 Agent 共用"
        actions={(
          <button
            type="button"
            onClick={refreshAll}
            className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            刷新
          </button>
        )}
      />

      {/* 会话区是页面主体：左会话列表、右只读消息，优先占据第一屏。 */}
      <section className="space-y-4">
        <SectionHeader
          icon={Sparkles}
          tone="indigo"
          title="对话历史"
          description="AI 接管过的会话与消息只读回放；接管次数为 AI 生成过回复的近似值"
        />
        <AgentTakeoverHistory />
      </section>

      {/* 设置区是附属配件：全部默认收起，展开后才占用空间。 */}
      <div className="space-y-3">
        <SettingsDisclosure
          icon={Sparkles}
          title="模型与凭据"
          description="按本地用户归属，已迁移到连接器页"
          tone="brand"
        >
          <div className="flex flex-col gap-3 rounded-xl bg-brand-50 p-4 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-sm leading-6 text-brand-900">
              AI 模型与 API Key 由当前用户的连接器配置提供；修改后立即对当前用户的全部闲鱼账号生效。
            </p>
            <button
              type="button"
              onClick={/* 点击跳转到连接器页维护模型与密钥。 */ () => onNavigate?.('connectors')}
              className="ios-btn-primary whitespace-nowrap rounded-xl px-4 py-2 font-bold"
            >
              前往连接器页
            </button>
          </div>
        </SettingsDisclosure>

        {/* AI 客服可配置接管范围：边界/语料/策略三层 JSON，改完随 AI 配置保存即时生效。 */}
        <AIConfigEditor settings={settings} onChange={/* patch 是 AI 客服配置编辑区写回的字段，合并进当前草稿。 */ patch => setSettings({ ...settings, ...patch })} />

        {/* AI 总闸：全局唯一总开关，关闭后所有账号的 AI 回复立即停止，不需要逐账号操作。 */}
        <SettingsDisclosure
          icon={Zap}
          title="AI 总闸"
          description="关闭后所有账号的 AI 回复立即停止，无需逐账号操作"
          tone="brand"
        >
          <label className="flex items-start gap-3 rounded-xl border border-brand-200 bg-brand-50 p-4 cursor-pointer">
            <input
              type="checkbox"
              className="mt-1"
              aria-label="启用 AI 自动回复"
              // 总闸未配置时按开启展示，与后端「缺省即开」的语义保持一致。
              checked={settings.ai_master_enabled !== false}
              onChange={/* 勾选变化更新 AI 总闸草稿；关闭后引擎对所有账号停止 AI 回复。 */ event => setSettings({ ...settings, ai_master_enabled: event.target.checked })}
            />
            <span>
              <span className="block text-sm font-bold text-brand-900">启用 AI 自动回复</span>
              <span className="mt-1 block text-xs leading-5 text-brand-800">
                关闭后所有账号的 AI 回复立即停止，无需逐账号操作；账号各自的开关值与砍价策略都会保留，重新打开总闸后按原设置恢复。
              </span>
            </span>
          </label>
        </SettingsDisclosure>

        {/* MCP 服务器列表：名称 + 服务地址行编辑，序列化到 mcp.servers JSON 字符串。 */}
        <MCPServerList settings={settings} onChange={/* patch 是 MCP 列表写回的字段，合并进当前草稿。 */ patch => setSettings({ ...settings, ...patch })} />

        {/* 能力档位：租户默认档位，未单独覆盖的账号按此生效；保存只提交档位本身。 */}
        <SettingsDisclosure
          icon={Bot}
          title="能力档位"
          description="无人监督、作用域锁定单个账号；是否运行随 AI 总闸统一生效"
          tone="brand"
        >
          <div className="space-y-5">
            {!agentSettings ? (
              <div className="p-6 text-center text-gray-400 space-y-3">
                {agentLoadError || (agentLoading ? '加载配置中...' : '暂无配置')}
                {!agentLoading && agentLoadError && (
                  <div>
                    <button type="button" className="ios-btn-primary px-4 py-2 rounded-xl" onClick={loadAgentSettings}>重新加载</button>
                  </div>
                )}
              </div>
            ) : (
              <>
                <div className="rounded-xl bg-brand-50 p-4">
                  <div className="font-bold text-gray-900">Agent 随 AI 统一开关生效</div>
                  <div className="text-xs text-gray-600 mt-1">
                    客服 Agent 不再有独立开关：AI 总闸与账号 AI 开关的组合结果，同时决定 AI 回复与客服 Agent 是否运行。
                    需要整体停用时关闭上方「AI 总闸」即可。
                  </div>
                </div>

                <div className="space-y-3">
                  <div className="text-sm font-bold text-gray-800">能力档位</div>
                  <div className="grid gap-3 md:grid-cols-3">
                    {agentSettings.presets.map(/* option 是服务端下发的当前档位选项。 */ option => (
                      <button
                        key={option.value}
                        type="button"
                        aria-pressed={agentSettings.preset === option.value}
                        onClick={/* 选中当前档位并写回草稿。 */ () => updateAgentDraft({ preset: option.value })}
                        className={`rounded-xl border p-4 text-left transition-colors ${agentSettings.preset === option.value ? 'border-brand-400 bg-brand-50' : 'border-gray-200 hover:border-gray-300'}`}
                      >
                        <span className="block text-sm font-bold text-gray-900">{option.label}</span>
                        <span className="mt-1 block text-xs leading-5 text-gray-600">{option.description}</span>
                      </button>
                    ))}
                  </div>
                  {agentSettings.presets.length === 0 && (
                    <p className="text-xs text-warning-700">服务端未下发档位名单，暂时无法切换档位，请确认服务端版本与本页面匹配。</p>
                  )}
                  <p className="text-xs leading-5 text-gray-500">
                    这里设置的是租户默认档位。单个卖家账号可在账号列表里单独覆盖，未覆盖的账号按此默认生效。
                    档位与能力内核一一对应，界面不另外维护一份工具清单。
                  </p>
                </div>

                <SaveBar
                  state={agentSaveBarState}
                  onSave={/* 提交租户默认档位。 */ () => void submitAgentSettings()}
                  label="保存"
                  scopeLabel="保存客服 Agent 配置"
                  successMessage={agentSaveMessage?.type === 'success' ? agentSaveMessage.text : '客服 Agent 配置已保存'}
                  errorMessage={agentSaveMessage?.type === 'error' ? agentSaveMessage.text : '保存客服 Agent 配置失败'}
                  sticky={false}
                  onSettled={settleAgentSaveBar}
                />
              </>
            )}
          </div>
        </SettingsDisclosure>
      </div>

      {/* 页级保存条：AI 配置只有一份草稿，提交动作与成功/失败反馈都在这里，不再用模态弹窗。 */}
      <SaveBar
        state={saveBarState}
        onSave={/* 点击主按钮或失败态重试都重新提交同一份 AI 草稿。 */ () => void handlePageSave()}
        label="保存 AI 配置"
        errorMessage={/* 使用 Hook 捕获的具体失败原因，未取到时回落到保存条默认文案。 */ saveError || undefined}
        successMessage="AI 配置已保存"
        sticky
        onSettled={settleSaveBar}
      />
    </PageShell>
  );
};

export default CustomerAgent;
