import {
  Check,
  ChevronDown,
  Eye,EyeOff,
  RefreshCw,
  Save,
  Sparkles,
  Zap
} from 'lucide-react';
import React from 'react';
import { DEFAULT_AI_API_URL } from '../constants';
import { useSettings } from '../hooks';
import { AIConfigEditor } from '../components/AIConfigEditor';
import { MCPServerList } from '../components/MCPServerList';

// AISettings 展示 AI 智能回复、AI 客服接管、人工确认开关与 MCP 服务器配置；保存只提交 AI 白名单字段。
const AISettings: React.FC = () => {
  // featureState 是 Settings Hook 提供的状态与动作集合，scope=ai 只保存 AI 白名单字段并启用模型发现与连接测试。
  const {
    settings, loading, loadError, saving, saveError, aiModels, modelsLoading, modelError, modelDropdownOpen,
    showApiKey, modelPickerRef, loadSettings, loadAIModels, testConnection, connectionTestLoading,
    connectionTestMessage, handleSave, setSettings, setModelDropdownOpen, setShowApiKey,
  } = useSettings('ai');

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
    <div className="max-w-6xl mx-auto space-y-8 animate-fade-in pb-24">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="w-12 h-12 bg-gray-100 rounded-2xl flex items-center justify-center">
              <Sparkles className="w-6 h-6 text-gray-600" />
          </div>
          <div>
              <h2 className="text-3xl font-extrabold text-gray-900">AI 设置</h2>
              <p className="text-gray-500 mt-1 text-sm font-medium">配置 AI 智能回复、客服接管范围与扩展服务</p>
          </div>
        </div>
        <button
          onClick={loadSettings}
          className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors"
        >
          <RefreshCw className="w-4 h-4" />
          刷新
        </button>
      </div>

      {saveError && (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">
          <span>{saveError}</span>
          <button type="button" className="font-bold underline" onClick={/* 点击重试提交 AI 白名单配置。 */ () => void handleSave()}>重试保存</button>
        </div>
      )}

      <div className="space-y-8">
        {/* AI Configuration */}
        <section className="space-y-4">
          <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
              <div className="p-1.5 rounded-lg bg-brand text-white">
                  <Sparkles className="w-4 h-4" />
              </div>
              AI 智能回复配置
          </h3>

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
                            className="w-full px-4 py-2.5 text-left text-sm text-gray-700 hover:bg-blue-50 hover:text-brand flex items-center justify-between gap-3"
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
                <p className="text-xs text-red-500">{modelError}</p>
              ) : (
                <p className="text-xs text-gray-500">
                  {aiModels.length > 0 ? `已从当前 API 地址读取到 ${aiModels.length} 个模型` : '模型列表从当前 API 地址读取，也可以手动输入模型名'}
                </p>
              )}
              {connectionTestMessage && (
                <div className={`rounded-lg px-3 py-2 text-xs font-medium ${connectionTestMessage.type === 'success' ? 'bg-green-50 text-green-700 border border-green-100' : 'bg-red-50 text-red-700 border border-red-100'}`}>
                  {connectionTestMessage.type === 'success' ? '✓ ' : '✗ '}{connectionTestMessage.text}
                </div>
              )}
            </div>

            <div className="p-3 bg-blue-50 rounded-xl text-xs text-blue-700">
              <strong>常见 AI 服务:</strong>
              <ul className="list-disc list-inside mt-1 space-y-0.5">
                <li>阿里云通义千问: https://dashscope.aliyuncs.com/compatible-mode/v1</li>
                <li>OpenAI: https://api.openai.com/v1</li>
              </ul>
            </div>
          </div>
        </section>

        {/* AI 客服可配置接管范围：边界/语料/策略三层 JSON，改完随 AI 配置保存即时生效。 */}
        <AIConfigEditor settings={settings} onChange={/* patch 是 AI 客服配置编辑区写回的字段，合并进当前草稿。 */ patch => setSettings({ ...settings, ...patch })} />

        {/* AI 回复人工确认：开关，开启后 AI 回复先拦截待人工确认再发送。 */}
        <section className="space-y-4">
          <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
            <div className="p-1.5 rounded-lg bg-blue-500 text-white">
              <Sparkles className="w-4 h-4" />
            </div>
            AI 回复人工确认
          </h3>
          <div className="ios-card rounded-xl p-6 bg-white">
            <label className="flex items-start gap-3 rounded-xl border border-blue-200 bg-blue-50 p-4 cursor-pointer">
              <input type="checkbox" className="mt-1" checked={settings.ai_reply_review_mode || false} onChange={/* 勾选变化更新 AI 回复人工确认开关草稿。 */ event => setSettings({ ...settings, ai_reply_review_mode: event.target.checked })} />
              <span>
                <span className="block text-sm font-bold text-blue-900">AI 回复人工确认模式</span>
                <span className="mt-1 block text-xs leading-5 text-blue-800">开启后 AI 生成的回复不会自动发送，需在会话页人工确认后再发出，防止误发。</span>
              </span>
            </label>
          </div>
        </section>

        {/* MCP 服务器列表：名称 + 服务地址行编辑，序列化到 mcp.servers JSON 字符串。 */}
        <MCPServerList settings={settings} onChange={/* patch 是 MCP 列表写回的字段，合并进当前草稿。 */ patch => setSettings({ ...settings, ...patch })} />
      </div>

      {/* Save Button */}
      <div className="fixed bottom-10 right-10 z-30">
        <button
            onClick={handleSave}
            disabled={saving}
            className="ios-btn-primary px-10 py-5 rounded-xl text-lg shadow-2xl shadow-blue-200 flex items-center gap-3 transform hover:scale-105 active:scale-95 transition-all disabled:opacity-70"
        >
            <Save className="w-6 h-6" />
            {saving ? '保存中...' : '保存 AI 配置'}
        </button>
      </div>
    </div>
  );
};

export default AISettings;
