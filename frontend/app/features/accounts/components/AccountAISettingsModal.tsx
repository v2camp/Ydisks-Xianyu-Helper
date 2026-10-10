import { Bot,Loader2,Save,Settings,ShieldCheck,X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import type { AccountAgentOverride,AccountAgentSupport,AccountDetail,AIReplySettings } from '../api';

/** INHERIT_AGENT_VALUE 是「跟随租户默认」在选择框里的取值；原生 select 不接受 null 作为选项值。 */
export const INHERIT_AGENT_VALUE = '__inherit__';

// AccountAISettingsModalProps 描述账号 AI 设置弹窗需要的状态和回调。
export interface AccountAISettingsModalProps {
  // account 是当前正在编辑 AI 设置的账号。
  account: AccountDetail;
  // settings 是账号 AI 设置编辑草稿。
  settings: AIReplySettings;
  // agentSupport 是账号级客服 Agent 授权草稿；读取失败时为 null，此时分组只给出不可用说明。
  agentSupport: AccountAgentSupport | null;
  // saving 表示 AI 设置保存请求是否正在执行。
  saving: boolean;
  // onChange 更新 AI 设置草稿。
  onChange: (settings: AIReplySettings) => void;
  // onAgentChange 更新账号级客服 Agent 授权草稿。
  onAgentChange: (patch: Partial<AccountAgentOverride>) => void;
  // onClose 关闭 AI 设置弹窗。
  onClose: () => void;
  // onSave 保存 AI 设置并刷新账号列表。
  onSave: () => void | Promise<void>;
}

// AccountAISettingsModal 渲染账号 AI 自动回复策略与账号级客服 Agent 授权编辑界面。
export const AccountAISettingsModal: React.FC<AccountAISettingsModalProps> = ({ account, settings, agentSupport, saving, onChange, onAgentChange, onClose, onSave }) => {
  // updateSettings 使用最新草稿合并单个 AI 字段变化。
  const updateSettings = (patch: Partial<AIReplySettings>) => onChange({ ...settings, ...patch });
  // handleEnabledChange 切换 AI 自动回复开关。
  const handleEnabledChange = () => updateSettings(settings.ai_enabled ? { ai_enabled: false, auto_adjust_price_enabled: false } : { ai_enabled: true });
  // handleAutoAdjustChange 切换真实订单自动改价开关，AI 议价关闭时不允许单独开启。
  const handleAutoAdjustChange = () => {
    if (!settings.ai_enabled) return;
    updateSettings({ auto_adjust_price_enabled: !settings.auto_adjust_price_enabled });
  };
  // handleDiscountPercentChange 更新最大折扣比例。
  const handleDiscountPercentChange = (event: React.ChangeEvent<HTMLInputElement>) => updateSettings({ max_discount_percent: parseInt(event.target.value, 10) || 0 });
  // handleDiscountAmountChange 更新最大折扣金额。
  const handleDiscountAmountChange = (event: React.ChangeEvent<HTMLInputElement>) => updateSettings({ max_discount_amount: parseInt(event.target.value, 10) || 0 });
  // handleBargainRoundsChange 更新最大砍价轮次。
  const handleBargainRoundsChange = (event: React.ChangeEvent<HTMLInputElement>) => updateSettings({ max_bargain_rounds: parseInt(event.target.value, 10) || 1 });
  // handlePromptChange 更新自定义 AI 提示词。
  const handlePromptChange = (event: React.ChangeEvent<HTMLTextAreaElement>) => updateSettings({ custom_prompts: event.target.value });
  // handleAgentEnabledChange 把启用状态选择写回账号级授权草稿，继承取值恢复为 null。
  const handleAgentEnabledChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
    // raw 是启用状态选择框的原始取值。
    const raw = event.target.value;
    onAgentChange({ enabled: raw === INHERIT_AGENT_VALUE ? null : raw === 'on' });
  };
  // handleAgentPresetChange 把档位选择写回账号级授权草稿，继承取值恢复为 null。
  const handleAgentPresetChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
    // raw 是档位选择框的原始取值。
    const raw = event.target.value;
    onAgentChange({ preset: raw === INHERIT_AGENT_VALUE ? null : raw });
  };
  // effectivePresetLabel 是账号当前生效档位的展示名；服务端名单里找不到时回落为标识本身。
  const effectivePresetLabel = agentSupport
    ? agentSupport.presets.find(/* option 是档位候选项，按 value 与账号当前生效档位匹配。 */ option => option.value === agentSupport.effectivePreset)?.label || agentSupport.effectivePreset
    : '';

  return createPortal(
    <div className="modal-overlay-centered">
      <div className="modal-container" style={{ maxWidth: '600px' }}>
        <div className="modal-header">
          <div>
            <h3 className="text-2xl font-extrabold text-gray-900 flex items-center gap-2"><Bot className="w-6 h-6 text-purple-500" />AI助手设置</h3>
            <p className="text-sm text-gray-500 mt-1">{account.nickname || account.remark || account.id}</p>
          </div>
          <button onClick={onClose} className="p-2 rounded-xl hover:bg-gray-100 transition-colors flex-shrink-0" aria-label="关闭 AI 设置"><X className="w-5 h-5 text-gray-500" /></button>
        </div>

        <div className="modal-body space-y-6">
          <div className="flex items-center justify-between p-4 bg-purple-50 rounded-xl">
            <div><div className="font-bold text-gray-900 flex items-center gap-2"><Bot className="w-4 h-4 text-purple-500" />启用AI自动回复</div><div className="text-xs text-gray-500">AI将自动处理买家的砍价消息</div></div>
            <button type="button" onClick={handleEnabledChange} className={`w-14 h-8 rounded-full transition-colors duration-300 relative ${settings.ai_enabled ? 'bg-brand' : 'bg-gray-300'}`} aria-label="切换 AI 自动回复">
              <span className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${settings.ai_enabled ? 'translate-x-6' : 'translate-x-0'}`} />
            </button>
          </div>

          <div className="flex items-center justify-between p-4 bg-amber-50 border border-amber-200 rounded-xl">
            <div className="pr-4"><div className="font-bold text-gray-900">自动执行 AI 报价改价</div><div className="text-xs text-gray-600 mt-1">AI 明确报价后，买家在 30 分钟内拍下对应商品时自动修改待付款订单价格。开启 AI 议价后，固定自动化规则改价将不能同时启用。</div></div>
            <button type="button" onClick={handleAutoAdjustChange} disabled={!settings.ai_enabled} className={`w-14 h-8 rounded-full transition-colors duration-300 relative flex-shrink-0 ${settings.auto_adjust_price_enabled ? 'bg-amber-500' : 'bg-gray-300'} ${!settings.ai_enabled ? 'opacity-50 cursor-not-allowed' : ''}`} aria-label="切换 AI 自动改价">
              <span className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${settings.auto_adjust_price_enabled ? 'translate-x-6' : 'translate-x-0'}`} />
            </button>
          </div>

          <div className="border-t border-gray-200 pt-6">
            <h3 className="text-lg font-bold text-gray-900 mb-4">砍价策略</h3>
            <div className="grid grid-cols-3 gap-4">
              <div><label className="block text-sm font-bold text-gray-700 mb-2">最大折扣比例 (%)</label><input type="number" value={settings.max_discount_percent} onChange={handleDiscountPercentChange} className="w-full ios-input px-4 py-3 rounded-xl" min="0" max="100" /><p className="text-xs text-gray-500 mt-1">例如：10 表示最多降价 10%；设为 0 表示不允许降价</p></div>
              <div><label className="block text-sm font-bold text-gray-700 mb-2">最大折扣金额 (元)</label><input type="number" value={settings.max_discount_amount} onChange={handleDiscountAmountChange} className="w-full ios-input px-4 py-3 rounded-xl" min="0" /><p className="text-xs text-gray-500 mt-1">例如：100 表示最多降价 100 元；设为 0 表示不允许降价</p></div>
              <div><label className="block text-sm font-bold text-gray-700 mb-2">最大砍价轮次</label><input type="number" value={settings.max_bargain_rounds} onChange={handleBargainRoundsChange} className="w-full ios-input px-4 py-3 rounded-xl" min="1" max="10" /><p className="text-xs text-gray-500 mt-1">买家最多可以砍价的次数</p></div>
            </div>
          </div>

          <div><label className="block text-sm font-bold text-gray-700 mb-2">自定义提示词（可选）</label><textarea value={settings.custom_prompts} onChange={handlePromptChange} placeholder="输入自定义的AI回复规则或风格指引...&#10;&#10;例如：回复时保持礼貌专业、使用简洁的语言、强调产品质量等" className="w-full ios-input px-4 py-3 rounded-xl h-40 resize-none" /></div>

          {/* 客服 Agent：能力授权与 AI 议价是两件事，因此单列分组，并且只暴露启用与档位两个控件。 */}
          <div className="border-t border-gray-200 pt-6 space-y-4">
            <h3 className="text-lg font-bold text-gray-900 flex items-center gap-2"><ShieldCheck className="w-5 h-5 text-sky-600" />客服 Agent</h3>
            {!agentSupport ? (
              <p className="text-xs text-gray-500">账号级客服 Agent 授权未能读取，保存时不会改动它；可以关闭弹窗后重试。</p>
            ) : (
              <>
                <div className="grid gap-4 md:grid-cols-2">
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2" htmlFor="account-agent-enabled">启用状态</label>
                    <select
                      id="account-agent-enabled"
                      aria-label="客服 Agent 启用状态"
                      value={agentSupport.enabled === null ? INHERIT_AGENT_VALUE : agentSupport.enabled ? 'on' : 'off'}
                      onChange={handleAgentEnabledChange}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                    >
                      <option value={INHERIT_AGENT_VALUE}>跟随租户默认（当前{agentSupport.effectiveEnabled ? '启用' : '关闭'}）</option>
                      <option value="on">启用</option>
                      <option value="off">关闭</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-sm font-bold text-gray-700 mb-2" htmlFor="account-agent-preset">能力档位</label>
                    <select
                      id="account-agent-preset"
                      aria-label="客服 Agent 能力档位"
                      value={agentSupport.preset === null ? INHERIT_AGENT_VALUE : agentSupport.preset}
                      onChange={handleAgentPresetChange}
                      className="w-full ios-input px-4 py-3 rounded-xl"
                    >
                      <option value={INHERIT_AGENT_VALUE}>跟随租户默认（当前{effectivePresetLabel || '未知'}）</option>
                      {agentSupport.presets.map(/* option 是服务端下发的当前档位选项。 */ option => (
                        <option key={option.value} value={option.value}>{option.label}</option>
                      ))}
                    </select>
                  </div>
                </div>
                <p className="text-xs leading-5 text-gray-500">
                  开启后客服 Agent 可自主查询该账号的订单与商品，并在所选档位允许的范围内对外应答。
                  这里的改动随下方「保存」一起提交，不再单独生效。
                </p>
                <p className="text-xs leading-5 text-gray-500">
                  档位只约束这一账号的能力上限，具体放行哪些操作由后端能力内核判定，界面不列出工具清单。
                </p>
              </>
            )}
          </div>

          <div className="bg-blue-50 border border-blue-200 rounded-xl p-4">
            <h4 className="font-bold text-blue-900 mb-2 flex items-center gap-2"><Settings className="w-4 h-4" />AI如何工作</h4>
            <ul className="text-xs text-blue-800 space-y-1"><li>• 自动识别买家的砍价请求</li><li>• 根据设定的策略智能回复</li><li>• 在合理范围内同意降价或礼貌拒绝</li><li>• 只有开启自动改价后，已发送给买家的有效报价才会用于真实订单改价</li></ul>
          </div>
        </div>

        <div className="modal-footer"><div className="flex gap-3 w-full">
          <button onClick={onClose} className="flex-1 px-6 py-3 rounded-xl font-bold bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors" disabled={saving}>取消</button>
          <button onClick={/* 当前回调处理用户交互或异步状态变化。 */ () => void onSave()} className="flex-1 ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center justify-center gap-2" disabled={saving}>{saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}{saving ? '保存中...' : '保存'}</button>
        </div></div>
      </div>
    </div>,
    document.body,
  );
};
