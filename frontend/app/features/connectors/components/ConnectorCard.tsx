import { ChevronDown, ChevronUp, ExternalLink, Eye, EyeOff, ShieldCheck } from 'lucide-react';
import React, { useState } from 'react';
import { StatusBadge } from '../../../../shared/ui/StatusBadge';
import type { SystemSettings } from '../api';
import { connectorReady } from '../state';
import type { ConnectorField, ConnectorMeta } from '../registry';

// ConnectorCardProps 描述连接器配置卡的受控输入。
interface ConnectorCardProps {
  // connector 是当前渲染的连接器定义，卡片所有控件都由它派生。
  connector: ConnectorMeta;
  // settings 是连接器配置草稿，读取各系统设置键的当前值。
  settings: SystemSettings;
  // loading 表示系统设置仍在读取；为真时状态徽章展示「读取中」，绝不展示「未配置」。
  loading?: boolean;
  // onChange 在任一控件变化后写回系统配置草稿补丁。
  onChange: (patch: Partial<SystemSettings>) => void;
}

// ConnectorFieldInput 渲染单个连接器字段，敏感字段默认掩码并可切换明文。
const ConnectorFieldInput: React.FC<{
  // field 是当前字段定义。
  field: ConnectorField;
  // value 是字段当前草稿值。
  value: string;
  // configured 表示敏感字段在服务端是否已配置。
  configured: boolean;
  // onChange 在输入变化后写回草稿补丁。
  onChange: (patch: Partial<SystemSettings>) => void;
}> = ({ field, value, configured, onChange }) => {
  // showSecret 控制敏感字段输入框是否明文显示；非敏感字段不使用该状态。
  const [showSecret, setShowSecret] = useState(false);
  // inputID 是字段输入框与标签关联使用的稳定标识。
  const inputID = `connector-field-${field.key.replace(/[^a-zA-Z0-9]+/g, '-')}`;
  // placeholder 是输入框提示，敏感字段已配置时改为替换提示。
  const placeholder = field.sensitive && configured ? `已配置，如需替换请输入新${field.label}` : field.placeholder;
  // inputType 是输入控件的 HTML 类型，敏感字段按明文开关在密码与文本间切换。
  const inputType = field.sensitive ? (showSecret ? 'text' : 'password') : 'text';

  return (
    <div className="space-y-2">
      <label className="block text-sm font-bold text-gray-800" htmlFor={inputID}>{field.label}</label>
      <div className="relative">
        <input
          id={inputID}
          aria-label={field.label}
          type={field.type === 'password' && !field.sensitive ? 'password' : inputType}
          value={value}
          onChange={/* 输入变化后把新值写回系统配置草稿。 */ event => onChange({ [field.key]: event.target.value })}
          placeholder={placeholder}
          autoComplete="off"
          className={`w-full ios-input px-4 py-3 rounded-xl font-mono text-sm${field.sensitive ? ' pr-12' : ''}`}
        />
        {field.sensitive && (
          <button
            type="button"
            onClick={/* 切换敏感字段明文显示状态。 */ () => setShowSecret(!showSecret)}
            className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600"
            title={showSecret ? `隐藏 ${field.label}` : `显示 ${field.label}`}
          >
            {showSecret ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
          </button>
        )}
      </div>
      {field.sensitive && configured && field.configuredHelp && (
        <p className="text-xs text-gray-500">{field.configuredHelp}</p>
      )}
    </div>
  );
};

// ConnectorCard 渲染连接器区的单个配置卡：凭据输入、平台引导、能力开关与平台限制提示。
export const ConnectorCard: React.FC<ConnectorCardProps> = ({ connector, settings, loading = false, onChange }) => {
  // Icon 是连接器定义中声明的图标组件。
  const Icon = connector.icon;
  // ready 表示当前草稿是否已具备该连接器的完整必需凭据。
  const ready = connectorReady(connector, settings);
  // title 是卡片的展示名称。
  const title = `${connector.label}连接器`;
  // guideOpenOverride 保存用户手动切换后的引导展开状态；null 表示用户尚未切换，继续跟随凭据就绪度。
  const [guideOpenOverride, setGuideOpenOverride] = useState<boolean | null>(null);
  // guideOpen 是引导区块的实际展开状态：凭据已就绪时默认折叠，未就绪时默认展开，手动切换后以手动状态为准。
  const guideOpen = guideOpenOverride ?? !ready;
  // statusTone 是状态徽章的语义色：读取中优先，其次是就绪与未配置。
  const statusTone = loading ? 'loading' : ready ? 'ready' : 'missing';
  // statusText 是状态徽章文案，读取期间不得显示「未配置」以免误判为没有配置。
  const statusText = loading ? '读取中' : ready ? '凭据已就绪' : '未配置';

  return (
    <div className="ios-card rounded-xl p-6 bg-white space-y-5">
      <div className="flex items-start gap-2">
        <div className="p-1.5 rounded-lg bg-brand-500 text-white mt-0.5">
          <Icon className="w-4 h-4" />
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <h4 className="text-sm font-bold text-gray-800">{title}</h4>
            <StatusBadge tone={statusTone} testId={`connector-status-${connector.id}`}>{statusText}</StatusBadge>
          </div>
          <p className="text-xs text-gray-500 mt-0.5">{connector.summary}</p>
        </div>
      </div>

      {connector.guide && (
        <div className="rounded-xl border border-brand-100 bg-brand-50 p-4 space-y-2">
          <div className="flex items-start justify-between gap-2">
            <p className="text-xs font-bold text-brand-900">{connector.guide.title}</p>
            <button
              type="button"
              aria-expanded={guideOpen}
              onClick={/* 手动切换引导展开状态，之后不再跟随凭据就绪度变化。 */ () => setGuideOpenOverride(!guideOpen)}
              className="inline-flex items-center gap-1 text-xs font-bold text-brand-700 hover:text-brand-900 shrink-0"
            >
              {guideOpen ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
              {guideOpen ? '收起' : '查看配置步骤'}
            </button>
          </div>
          {guideOpen && (
            <>
              <ol className="text-xs leading-5 text-brand-900 list-decimal pl-4 space-y-1">
                {connector.guide.steps.map(/* step 是当前引导步骤文案，index 用于生成稳定 key。 */ (step, index) => (
                  <li key={index}>{step}</li>
                ))}
              </ol>
              {connector.guide.url && (
                <a
                  href={connector.guide.url}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center gap-1 text-xs font-bold text-brand-700 hover:text-brand-900 underline"
                >
                  <ExternalLink className="w-3.5 h-3.5" />
                  {connector.guide.urlLabel || '前往平台'}
                </a>
              )}
            </>
          )}
        </div>
      )}

      {connector.fields.map(/* field 是当前连接器的凭据字段定义。 */ field => {
        // configured 表示该敏感字段的服务端「已配置」派生标记是否命中。
        const configured = field.configuredKey ? settings[field.configuredKey] === true : false;
        return (
          <ConnectorFieldInput
            key={field.key}
            field={field}
            value={String(settings[field.key] ?? '')}
            configured={configured}
            onChange={onChange}
          />
        );
      })}

      {connector.notices?.map(/* notice 是当前连接器的一条平台限制提示。 */ (notice, index) => (
        <div key={index} className="flex items-start gap-2 rounded-xl bg-warning-50 px-3 py-2.5">
          <ShieldCheck className="w-4 h-4 mt-0.5 text-warning-600 flex-shrink-0" />
          <p className="text-xs leading-5 text-warning-800">{notice}</p>
        </div>
      ))}

      {connector.capabilities.map(/* capability 是当前连接器的一项能力定义。 */ capability => {
        // enabled 表示能力总开关是否打开。
        const enabled = settings[capability.toggleKey] === true;
        return (
          <div key={capability.id} className="space-y-4">
            <label className="flex items-start gap-3 rounded-xl border border-brand-200 bg-brand-50 p-4 cursor-pointer" htmlFor={`connector-capability-${connector.id}-${capability.id}`}>
              <input
                id={`connector-capability-${connector.id}-${capability.id}`}
                type="checkbox"
                aria-label={`启用 ${connector.label} ${capability.label.replace(/^启用/, '')}`}
                className="mt-1"
                checked={enabled}
                onChange={/* 能力开关变化后写回系统配置草稿。 */ event => onChange({ [capability.toggleKey]: event.target.checked })}
              />
              <span>
                <span className="block text-sm font-bold text-brand-900">{capability.label}</span>
                <span className="mt-1 block text-xs leading-5 text-brand-800">{capability.description}</span>
              </span>
            </label>

            {enabled && capability.allowlistKey && (
              <div className="space-y-2">
                <label className="block text-sm font-bold text-gray-800" htmlFor={`connector-allowlist-${connector.id}-${capability.id}`}>{capability.allowlistLabel}</label>
                <textarea
                  id={`connector-allowlist-${connector.id}-${capability.id}`}
                  aria-label={capability.allowlistLabel}
                  value={String(settings[capability.allowlistKey] ?? '')}
                  onChange={/* 白名单输入变化后写回系统配置草稿。 */ event => onChange({ [capability.allowlistKey as string]: event.target.value })}
                  placeholder={capability.allowlistPlaceholder}
                  className="w-full ios-input px-4 py-3 rounded-xl h-24 text-sm leading-6 font-mono resize-y"
                />
                {capability.allowlistHelp && <p className="text-xs text-gray-500">{capability.allowlistHelp}</p>}
              </div>
            )}
          </div>
        );
      })}

      <p className="text-xs text-gray-500">{connector.effectNote}</p>
    </div>
  );
};
