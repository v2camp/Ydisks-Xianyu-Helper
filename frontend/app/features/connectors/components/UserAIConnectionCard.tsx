import { Check,Eye,EyeOff,RefreshCw,Sparkles,Zap } from 'lucide-react';
import React,{ useCallback,useState } from 'react';
import { StatusBadge } from '../../../../shared/ui/StatusBadge';
import type { SystemSettings } from '../api';
import { fetchUserAIModels,testUserAIConnection } from '../api';
import { isAIReady } from '../state';

/** UserAIConnectionCardProps 描述用户级 AI 模型与密钥卡的受控输入。 */
interface UserAIConnectionCardProps {
  /** settings 是当前用户的集成配置草稿。 */
  settings: SystemSettings;
  /** onChange 在任一字段变化后写回草稿补丁。 */
  onChange: (patch: Partial<SystemSettings>) => void;
}

/** UserAIConnectionCard 渲染当前用户的 AI API 地址、密钥、模型与连接测试。 */
export const UserAIConnectionCard: React.FC<UserAIConnectionCardProps> = ({ settings, onChange }) => {
  // showKey 控制 API Key 是否明文显示。
  const [showKey, setShowKey] = useState(false);
  // models 保存模型目录读取结果。
  const [models, setModels] = useState<string[]>([]);
  // modelsLoading 表示模型目录读取是否进行中。
  const [modelsLoading, setModelsLoading] = useState(false);
  // testLoading 表示连接测试是否进行中。
  const [testLoading, setTestLoading] = useState(false);
  // testMessage 保存连接测试的成功或失败提示。
  const [testMessage, setTestMessage] = useState<{ /** 提示类型。 */ type: 'success' | 'error'; /** 提示正文。 */ text: string } | null>(null);
  // ready 表示当前草稿是否已具备模型与密钥。
  const ready = isAIReady(settings);

  // loadModels 使用当前草稿读取模型目录。
  const loadModels = useCallback(/* 读取回调只在用户点击时触发。 */ async () => {
    setModelsLoading(true);
    setTestMessage(null);
    try {
      // result 是当前 API 地址返回的模型名称。
      const result = await fetchUserAIModels(String(settings.ai_api_url || ''), String(settings.ai_api_key || ''));
      setModels(result);
    } catch (/* error 是模型目录读取失败原因，不包含密钥。 */ error) {
      console.error('读取用户 AI 模型失败', error);
      setTestMessage({ type: 'error', text: '读取模型失败，请检查 API 地址与密钥' });
    } finally {
      setModelsLoading(false);
    }
  }, [settings.ai_api_key, settings.ai_api_url]);

  // testConnection 使用当前草稿发起一次最小连接测试。
  const testConnection = useCallback(/* 测试回调只在用户点击时触发。 */ async () => {
    setTestLoading(true);
    setTestMessage(null);
    try {
      // result 是连接测试结果。
      const result = await testUserAIConnection(String(settings.ai_api_url || ''), String(settings.ai_api_key || ''), String(settings.ai_model || ''));
      setTestMessage({ type: 'success', text: `连接成功：${result.model} · ${result.latency_ms}ms` });
    } catch (/* error 是连接测试失败原因，不包含密钥。 */ error) {
      console.error('测试用户 AI 连接失败', error);
      setTestMessage({ type: 'error', text: '连接失败，请检查 API 地址、密钥与模型' });
    } finally {
      setTestLoading(false);
    }
  }, [settings.ai_api_key, settings.ai_api_url, settings.ai_model]);

  return (
    <div className="ios-card rounded-xl p-6 bg-white space-y-5">
      <div className="flex items-start gap-2">
        <div className="p-1.5 rounded-lg bg-brand-500 text-white mt-0.5"><Sparkles className="w-4 h-4" /></div>
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <h4 className="text-sm font-bold text-gray-800">AI 模型与密钥</h4>
            <StatusBadge tone={ready ? 'ready' : 'missing'} testId="user-ai-status">{ready ? '已就绪' : '未配置'}</StatusBadge>
          </div>
          <p className="text-xs text-gray-500 mt-0.5">当前用户的闲鱼账号共用这份模型配置；密钥只加密保存在本机。</p>
        </div>
      </div>

      <div className="space-y-3">
        <label className="block text-sm font-bold text-gray-800" htmlFor="user-ai-api-url">API 地址</label>
        <input
          id="user-ai-api-url"
          type="text"
          value={String(settings.ai_api_url || '')}
          onChange={/* 输入变化写回 API 地址草稿。 */ event => onChange({ ai_api_url: event.target.value })}
          placeholder="https://api.openai.com/v1"
          className="w-full ios-input px-4 py-3 rounded-xl text-sm"
        />
        <p className="text-xs text-gray-500">无需补全 /chat/completions</p>
      </div>

      <div className="space-y-3">
        <label className="block text-sm font-bold text-gray-800" htmlFor="user-ai-api-key">API Key</label>
        <div className="relative">
          <input
            id="user-ai-api-key"
            type={showKey ? 'text' : 'password'}
            value={String(settings.ai_api_key || '')}
            onChange={/* 输入变化写回 API Key 草稿；该值只存在于会话状态。 */ event => onChange({ ai_api_key: event.target.value })}
            placeholder={settings.ai_api_key_configured === true ? '已配置，如需替换请输入新密钥' : 'sk-...'}
            className="w-full ios-input px-4 py-3 pr-12 rounded-xl font-mono text-sm"
          />
          <button
            type="button"
            onClick={/* 点击切换 API Key 明文显示。 */ () => setShowKey(!showKey)}
            className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600"
            title={showKey ? '隐藏 API Key' : '显示 API Key'}
          >
            {showKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
          </button>
        </div>
      </div>

      <div className="space-y-3">
        <label className="block text-sm font-bold text-gray-800" htmlFor="user-ai-model">模型</label>
        <div className="flex flex-col sm:flex-row gap-2">
          <input
            id="user-ai-model"
            type="text"
            value={String(settings.ai_model || '')}
            onChange={/* 输入变化写回模型名称草稿。 */ event => onChange({ ai_model: event.target.value })}
            placeholder="从接口读取或手动输入模型名"
            list="user-ai-model-options"
            className="flex-1 ios-input px-4 py-3 rounded-xl"
          />
          <datalist id="user-ai-model-options">
            {models.map(/* model 是当前可选的模型名称。 */ model => <option key={model} value={model} />)}
          </datalist>
          <button
            type="button"
            onClick={/* 点击读取模型目录。 */ () => void loadModels()}
            disabled={modelsLoading}
            className="px-4 py-3 rounded-xl bg-gray-100 text-gray-700 hover:bg-gray-200 disabled:opacity-60 font-bold flex items-center justify-center gap-2 whitespace-nowrap"
          >
            <RefreshCw className={`w-4 h-4${modelsLoading ? ' animate-spin' : ''}`} />
            读取模型
          </button>
          <button
            type="button"
            onClick={/* 点击发起连接测试。 */ () => void testConnection()}
            disabled={testLoading}
            className="px-4 py-3 rounded-xl bg-gray-100 text-gray-700 hover:bg-gray-200 disabled:opacity-60 font-bold flex items-center justify-center gap-2 whitespace-nowrap"
          >
            <Zap className={`w-4 h-4${testLoading ? ' animate-spin' : ''}`} />
            测试连接
          </button>
        </div>
        {models.length > 0 && <p className="text-xs text-gray-500 flex items-center gap-1"><Check className="w-3.5 h-3.5 text-success-500" />已读取 {models.length} 个模型</p>}
        {testMessage && <p className={`text-xs ${testMessage.type === 'success' ? 'text-success-600' : 'text-danger-500'}`}>{testMessage.text}</p>}
      </div>
    </div>
  );
};
