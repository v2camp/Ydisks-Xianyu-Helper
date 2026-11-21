import { Bell,Check,Loader2,X } from 'lucide-react';
import React from 'react';
import { createPortal } from 'react-dom';
import type { NotificationChannel,NotificationChannelType,NotificationEventType } from '../api';
import { notificationChannelTypes } from '../state';
import type { NotificationForm } from '../types';
import type { NotificationEventMeta } from '../types';
import { NotificationEventSelector } from './NotificationEventSelector';

// NotificationChannelModalProps 描述渠道编辑弹窗所需的状态和回调。
export interface NotificationChannelModalProps {
  // showModal 表示弹窗是否打开。
  showModal: boolean;
  // editing 表示当前编辑的渠道，新增时为空。
  editing: NotificationChannel | null;
  // form 是当前渠道表单值。
  form: NotificationForm;
  // setForm 更新渠道表单值。
  setForm: React.Dispatch<React.SetStateAction<NotificationForm>>;
  // saving 表示渠道保存请求是否正在执行。
  saving: boolean;
  // savedCredentialKeys 是当前渠道已保存的凭据键名，这些字段可以留空并沿用服务端原值。
  savedCredentialKeys?: string[];
  // resetCredentialState 在渠道类型切换时清空上一类型的凭据状态。
  resetCredentialState: () => void;
  // onClose 关闭弹窗。
  onClose: () => void;
  // onSave 提交渠道表单。
  onSave: () => void | Promise<void>;
  // events 是可绑定事件清单；系统告警渠道只展示实例级事件。
  events?: NotificationEventMeta[];
}

// NotificationChannelModal 渲染渠道字段和事件绑定表单；连接器级凭据统一在用户集成配置中维护。
export const NotificationChannelModal: React.FC<NotificationChannelModalProps> = ({ showModal, editing, form, setForm, saving, savedCredentialKeys = [], resetCredentialState, onClose, onSave, events }) => {
  // meta 是当前渠道类型对应的字段和指南。
  const meta = notificationChannelTypes[form.type];
  // credentialKeys 是当前渠道类型声明的凭据键，只有这些键参与留空沿用与清除。
  const credentialKeys = meta.credential.keys;
  // hasSavedCredentials 表示当前渠道仍保存着本类型的独立凭据，留空保存会沿用它们。
  const hasSavedCredentials = savedCredentialKeys.some(
    // key 是当前待判定的已保存键名。
    key => credentialKeys.includes(key),
  );
  // inheritsConnectorCredentials 表示用户已明确改用用户级集成配置，保存时由载荷把这些键写成空串。
  const inheritsConnectorCredentials = form.credentialAction === 'clear';
  // handleNameChange 更新渠道名称。
  const handleNameChange = (event: React.ChangeEvent<HTMLInputElement>) => setForm(
    // nextFormUpdater 使用函数式更新名称字段。
    current => ({ ...current, name: event.target.value }),
  );
  // handleTypeChange 切换渠道类型，清空旧类型配置并复位凭据状态，避免沿用上一类型的已保存键。
  const handleTypeChange = (event: React.MouseEvent<HTMLButtonElement>) => {
    // type 是按钮数据属性中的目标渠道类型。
    const type = event.currentTarget.dataset.type as NotificationChannelType | undefined;
    // 重复点击当前类型不应丢弃已填配置与凭据状态。
    if (!type || type === form.type) return;
    setForm(
      // nextFormUpdater 使用函数式更新渠道类型。
      current => ({ ...current, type, config: {} }),
    );
    resetCredentialState();
  };
  // handleConfigFieldChange 更新渠道配置字段。
  const handleConfigFieldChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    // key 是输入框数据属性中的配置字段名。
    const key = event.currentTarget.dataset.field;
    if (!key) return;
    setForm(
      // nextFormUpdater 使用函数式更新配置字段。
      current => ({ ...current, config: { ...current.config, [key]: event.target.value } }),
    );
  };
  // handleToggleEvent 使用函数式更新切换事件绑定。
  const handleToggleEvent = (event: NotificationEventType) => setForm(
    // nextFormUpdater 使用函数式更新事件绑定集合。
    current => ({ ...current, event_types: current.event_types.includes(event) ? current.event_types.filter(
      // item 是待保留的已有事件值。
      item => item !== event,
    ) : [...current.event_types, event] }),
  );
  // handleToggleEnabled 使用函数式更新切换渠道启用状态。
  const handleToggleEnabled = () => setForm(
    // nextFormUpdater 使用函数式更新启用状态。
    current => ({ ...current, enabled: !current.enabled }),
  );
  // handleToggleConnectorCredentials 切换是否改用用户级集成凭据：勾选只记录清除意图，具体空串由保存载荷生成。
  const handleToggleConnectorCredentials = (event: React.ChangeEvent<HTMLInputElement>) => {
    // checked 表示用户是否勾选改用用户级集成凭据。
    const checked = event.target.checked;
    setForm(
      // nextFormUpdater 勾选时记录清除意图，取消勾选时显式回到「沿用渠道凭据」。
      current => ({ ...current, credentialAction: checked ? 'clear' : 'retain' }),
    );
  };
  // renderGuideStep 渲染当前渠道指南中的单个步骤。
  const renderGuideStep = (step: string, index: number) => <li key={index}>{step}</li>;

  if (!showModal) return null;

  return createPortal(
    <div className="modal-overlay">
      <div className="modal-container" style={{ maxWidth: '36rem' }}>
        <div className="px-6 py-5 border-b border-gray-100 flex items-center justify-between"><h3 className="text-xl font-extrabold text-gray-900">{editing ? '编辑通知渠道' : '新建通知渠道'}</h3><button onClick={onClose} className="w-10 h-10 rounded-2xl bg-gray-100 hover:bg-gray-200 flex items-center justify-center"><X className="w-5 h-5 text-gray-600" /></button></div>
        <div className="px-6 py-5 space-y-5 overflow-y-auto flex-1 min-h-0">
          <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">渠道名称</label><input type="text" value={form.name} onChange={handleNameChange} placeholder="例如：我的 Bark" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div>
          <div className="space-y-2">
            <label className="block text-sm font-bold text-gray-800">渠道类型</label>
            <div className="grid grid-cols-3 sm:grid-cols-4 gap-2">
              {(Object.keys(notificationChannelTypes) as NotificationChannelType[]).map(
                // type 是当前渠道类型选择项。
                type => {
                  // channelMeta 是当前类型的展示元数据。
                  const channelMeta = notificationChannelTypes[type];
                  // TypeIcon 是当前类型的图标组件。
                  const TypeIcon = channelMeta.icon;
                  // selected 表示当前类型是否已选中。
                  const selected = form.type === type;
                  return <button key={type} data-type={type} type="button" onClick={handleTypeChange} className={`p-3 rounded-xl border-2 flex flex-col items-center gap-1.5 transition-all ${selected ? 'border-brand bg-brand-50' : 'border-gray-100 hover:border-gray-300'}`}><TypeIcon className={`w-5 h-5 ${selected ? 'text-brand' : 'text-gray-500'}`} /><span className={`text-xs font-bold ${selected ? 'text-brand' : 'text-gray-600'}`}>{channelMeta.label}</span></button>;
                },
              )}
            </div>
          </div>
          <div className="rounded-xl bg-warning-50 border border-warning-200 p-4 space-y-2"><div className="flex items-center gap-2"><Bell className="w-4 h-4 text-warning-600" /><span className="text-sm font-bold text-warning-800">如何获取 {meta.label} 配置？</span></div><ol className="space-y-1.5 text-xs text-warning-900/90 leading-5 list-decimal pl-5">{meta.guide.steps.map(renderGuideStep)}</ol>{meta.guide.urlFormat && <div className="text-xs text-warning-900/90"><span className="font-bold">格式示例：</span><code className="mt-1 block bg-warning-100/70 px-2.5 py-1.5 rounded-lg break-all font-mono text-[11px]">{meta.guide.urlFormat}</code></div>}{meta.guide.note && <p className="text-xs text-warning-800/80 leading-5 pt-1">💡 {meta.guide.note}</p>}</div>
          {hasSavedCredentials && (
            <div className="rounded-xl border border-gray-200 bg-gray-50 p-4 space-y-2 text-xs leading-5 text-gray-700">
              <p>当前渠道仍保存着独立凭据，继续保存将沿用它们。</p>
              <label className="flex items-center gap-2 font-bold"><input type="checkbox" checked={inheritsConnectorCredentials} onChange={handleToggleConnectorCredentials} />改用用户级集成凭据</label>
            </div>
          )}
          <div className="space-y-3">
            {meta.fields.map(
              // field 是当前渠道配置字段。
              field => {
                // optionalCredential 表示该凭据字段在当前场景可以留空，服务端已保存的值会被沿用。
                const optionalCredential = savedCredentialKeys.includes(field.key) && credentialKeys.includes(field.key);
                return <div key={field.key} className="space-y-2"><label className="block text-sm font-bold text-gray-800">{field.label}{field.required && !optionalCredential && <span className="text-danger-500 ml-1">*</span>}</label><input data-field={field.key} type={field.type === 'password' ? 'password' : field.type === 'number' ? 'number' : 'text'} value={String(form.config[field.key] || '')} onChange={handleConfigFieldChange} placeholder={optionalCredential ? '已保存，留空则保持原值' : field.placeholder} className="w-full ios-input px-4 py-3 rounded-xl text-sm" />{(optionalCredential || field.help) && <p className="text-xs text-gray-500">{optionalCredential ? '已保存，留空则保持原值；填写新值会覆盖服务端保存的凭据。' : field.help}</p>}</div>;
              },
            )}
          </div>
          {form.type === 'email' && (
            <div className="rounded-xl border border-brand-100 bg-brand-50 p-4 text-sm leading-6 text-gray-700">
              邮件渠道使用「当前用户的集成配置 → 邮件发送（SMTP）」中的发件配置，本弹窗只设置收件邮箱。
            </div>
          )}
          <NotificationEventSelector selectedEvents={form.event_types} onToggleEvent={handleToggleEvent} events={events} />
          <label className="flex items-center gap-3 cursor-pointer"><button type="button" onClick={handleToggleEnabled} className={`relative w-11 h-6 rounded-full transition-colors ${form.enabled ? 'bg-brand' : 'bg-gray-300'}`}><span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full transition-transform ${form.enabled ? 'translate-x-5' : ''}`} /></button><span className="text-sm font-bold text-gray-800">启用此渠道</span></label>
        </div>
        <div className="px-6 py-4 border-t border-gray-100 flex items-center justify-end gap-3"><button onClick={onClose} className="px-5 py-2.5 rounded-xl bg-gray-100 hover:bg-gray-200 font-bold text-gray-700 transition-colors">取消</button><button onClick={onSave} disabled={saving} className="ios-btn-primary px-6 py-2.5 rounded-xl font-bold flex items-center gap-2 disabled:opacity-50">{saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Check className="w-4 h-4" />}{saving ? '保存中...' : '保存'}</button></div>
      </div>
    </div>,
    document.body,
  );
};
