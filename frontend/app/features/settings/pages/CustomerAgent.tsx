import {
  Bot,
  Check,
  ChevronDown,
  Eye,EyeOff,
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
import { COMMON_AI_SERVICES,DEFAULT_AI_API_URL } from '../constants';
import { useSettings } from '../hooks';
import { useCustomerAgent } from '../customerAgentHooks';
import { AIConfigEditor } from '../components/AIConfigEditor';
import { AgentTakeoverHistory } from '../components/AgentTakeoverHistory';
import { MCPServerList } from '../components/MCPServerList';

// CustomerAgent 汇总客服 Agent 的 AI 引擎、接管范围、出站 MCP 工具、能力档位与接管历史；
// 页级保存只提交 AI 白名单字段，能力档位走自己的区块级保存条。
const CustomerAgent: React.FC = () => {
  // featureState 是 Settings Hook 提供的状态与动作集合，scope=ai 只保存 AI 白名单字段并启用模型发现与连接测试。
  const {
    settings, loading, loadError, saveError, aiModels, modelsLoading, modelError, modelDropdownOpen,
    showApiKey, modelPickerRef, loadSettings, loadAIModels, testConnection, connectionTestLoading,
    connectionTestMessage, handleSave, setSettings, setModelDropdownOpen, setShowApiKey,
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

  // currentModel 是当前配置中的模型名称。
  const currentModel = settings.ai_model || '';
  // visibleAIModels 是模型下拉框当前展示的候选列表。
  const visibleAIModels = aiModels;

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

      <div className="space-y-8">
        {/* AI Configuration */}
        <section className="space-y-4">
          <SectionHeader icon={Sparkles} title="模型与凭据" tone="brand" />

          <div className="ios-card rounded-xl p-6 bg-white space-y-6">
            <div className="space-y-3">
              <label className="block text-sm font-bold text-gray-800">API 地址</label>
              <input
                type="text"
                value={settings.ai_api_url || DEFAULT_AI_API_URL}
                onChange={/* 输入变化更新 AI 服务基础地址草稿。 */ e => setSettings({...settings, ai_api_url: e.target.value})}
                className="w-full ios-input px-4 py-3 rounded-xl text-sm"
                placeholder="https://api.openai.com/v1"
              />
              <p className="text-xs text-gray-500">无需补全 /chat/completions</p>
            </div>

            <div className="space-y-3">
              <label className="block text-sm font-bold text-gray-800">API Key</label>
              <div className="relative">
                <input
                  type={showApiKey ? 'text' : 'password'}
                  value={settings.ai_api_key || ''}
                  onChange={/* 输入变化更新 AI API 密钥草稿；该值仅存于会话状态不写入日志。 */ e => setSettings({...settings, ai_api_key: e.target.value})}
                  className="w-full ios-input px-4 py-3 pr-12 rounded-xl font-mono text-sm"
                  placeholder={settings.ai_api_key_configured ? '已配置，如需替换请输入新密钥' : 'sk-...'}
                />
                <button
                  type="button"
                  onClick={/* 点击切换 API Key 明文显示状态。 */ () => setShowApiKey(!showApiKey)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600 transition-colors"
                >
                  {showApiKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>
            </div>

            <div className="space-y-3">
              <label className="block text-sm font-bold text-gray-800">模型</label>
              <div ref={modelPickerRef} className="relative flex flex-col sm:flex-row gap-2">
                <div className="relative flex-1">
                  <input
                    value={currentModel}
                    onFocus={/* 聚焦时若有已读取模型则展开下拉列表。 */ () => aiModels.length > 0 && setModelDropdownOpen(true)}
                    onChange={/* 输入变化更新模型名称草稿并展开候选列表。 */ e => {
                      setSettings({...settings, ai_model: e.target.value});
                      if (aiModels.length > 0) setModelDropdownOpen(true);
                    }}
                    onKeyDown={/* 键盘操作关闭或展开模型下拉列表。 */ e => {
                      if (e.key === 'Escape') setModelDropdownOpen(false);
                      if (e.key === 'ArrowDown' && aiModels.length > 0) setModelDropdownOpen(true);
                    }}
                    className="w-full ios-input px-4 py-3 pr-10 rounded-xl"
                    placeholder="从接口读取或手动输入模型名"
                  />
                  <button
                    type="button"
                    onClick={/* 点击切换模型下拉列表展开状态。 */ () => aiModels.length > 0 && setModelDropdownOpen(/* open 是切换前的展开状态，返回取反后的下一状态。 */ open => !open)}
                    disabled={aiModels.length === 0}
                    className="absolute right-2 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600 disabled:opacity-30"
                    aria-label="展开模型列表"
                  >
                    <ChevronDown className={`w-4 h-4 transition-transform ${modelDropdownOpen ? 'rotate-180' : ''}`} />
                  </button>
                  {modelDropdownOpen && (
                    <div className="absolute left-0 right-0 top-[calc(100%+6px)] z-40 max-h-64 overflow-y-auto rounded-xl border border-gray-200 bg-white shadow-xl shadow-gray-200/70 py-1">
                      {visibleAIModels.length > 0 ? (
                        visibleAIModels.map(/* model 是远端返回的单个模型名称，点击后写入草稿。 */ model => (
                          <button
                            key={model}
                            type="button"
                            onClick={/* 点击候选模型写入草稿并收起下拉列表。 */ () => {
                              setSettings({...settings, ai_model: model});
                              setModelDropdownOpen(false);
                            }}
                            className="w-full px-4 py-2.5 text-left text-sm text-gray-700 hover:bg-brand-50 hover:text-brand flex items-center justify-between gap-3"
                          >
                            <span className="truncate">{model}</span>
                            {model === currentModel && <Check className="w-4 h-4 shrink-0 text-brand" />}
                          </button>
                        ))
                      ) : (
                        <div className="px-4 py-3 text-sm text-gray-400">没有匹配的模型</div>
                      )}
                    </div>
                  )}
                </div>
                <button
                  type="button"
                  onClick={/* 点击向当前 API 地址发起模型发现请求。 */ () => loadAIModels(undefined, true)}
                  disabled={modelsLoading}
                  className="px-4 py-3 rounded-xl bg-gray-100 text-gray-700 hover:bg-gray-200 disabled:opacity-60 font-bold flex items-center justify-center gap-2 whitespace-nowrap"
                >
                  <RefreshCw className={`w-4 h-4 ${modelsLoading ? 'animate-spin' : ''}`} />
                  读取模型
                </button>
                <button
                  type="button"
                  onClick={/* 点击发起一次 AI 连接测试。 */ testConnection}
                  disabled={connectionTestLoading}
                  className="px-4 py-3 rounded-xl bg-gray-100 text-gray-700 hover:bg-gray-200 disabled:opacity-60 font-bold flex items-center justify-center gap-2 whitespace-nowrap cursor-pointer"
                >
                  <Zap className={`w-4 h-4 ${connectionTestLoading ? 'animate-spin' : ''}`} />
                  测试连接
                </button>
              </div>
              {modelError ? (
                <p className="text-xs text-danger-500">{modelError}</p>
              ) : (
                <p className="text-xs text-gray-500">
                  {aiModels.length > 0 ? `已从当前 API 地址读取到 ${aiModels.length} 个模型` : '模型列表从当前 API 地址读取，也可以手动输入模型名'}
                </p>
              )}
              {connectionTestMessage && (
                <div className={`rounded-lg px-3 py-2 text-xs font-medium ${connectionTestMessage.type === 'success' ? 'bg-success-50 text-success-700 border border-success-100' : 'bg-danger-50 text-danger-700 border border-danger-100'}`}>
                  {connectionTestMessage.type === 'success' ? '✓ ' : '✗ '}{connectionTestMessage.text}
                </div>
              )}
            </div>

            {/* 常见 AI 服务提示：默认折叠，展开后才列出兼容地址；两条示例来自常量，避免散落在 JSX 里。 */}
            <details className="p-3 bg-brand-50 rounded-xl text-xs text-brand-700">
              <summary className="cursor-pointer font-bold select-none">常见 AI 服务</summary>
              <ul className="list-disc list-inside mt-2 space-y-0.5">
                {COMMON_AI_SERVICES.map(/* service 是一条参考服务及其兼容 API 地址。 */ service => (
                  <li key={service.url}>{service.name}: {service.url}</li>
                ))}
              </ul>
            </details>
          </div>
        </section>

        {/* AI 客服可配置接管范围：边界/语料/策略三层 JSON，改完随 AI 配置保存即时生效。 */}
        <AIConfigEditor settings={settings} onChange={/* patch 是 AI 客服配置编辑区写回的字段，合并进当前草稿。 */ patch => setSettings({ ...settings, ...patch })} />

        {/* AI 总闸：全局唯一总开关，关闭后所有账号的 AI 回复立即停止，不需要逐账号操作。 */}
        <section className="space-y-4">
          <SectionHeader icon={Zap} title="AI 总闸" tone="brand" />
          <div className="ios-card rounded-xl p-6 bg-white">
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
          </div>
        </section>

        {/* MCP 服务器列表：名称 + 服务地址行编辑，序列化到 mcp.servers JSON 字符串。 */}
        <MCPServerList settings={settings} onChange={/* patch 是 MCP 列表写回的字段，合并进当前草稿。 */ patch => setSettings({ ...settings, ...patch })} />

        {/* 能力档位：租户默认档位，未单独覆盖的账号按此生效；保存只提交档位本身。 */}
        <section className="space-y-4">
          <SectionHeader
            icon={Bot}
            tone="brand"
            title="能力档位"
            description="无人监督、作用域锁定单个账号；是否运行随 AI 总闸统一生效，这里只配置能力档位"
          />
          <div className="ios-card rounded-xl p-6 bg-white space-y-5">
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
        </section>

        {/* 对话历史：AI 接管过的会话只读回放，不参与页级保存。 */}
        <section className="space-y-4">
          <SectionHeader
            icon={Sparkles}
            tone="indigo"
            title="对话历史"
            description="AI 接管过的会话与消息只读回放；接管次数为 AI 生成过回复的近似值"
          />
          <AgentTakeoverHistory />
        </section>
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
