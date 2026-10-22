import { Eye,EyeOff,Mail } from 'lucide-react';
import React from 'react';
import { SaveBar } from '../../../../shared/ui/SaveBar';
import type { SaveBarState } from '../../../../shared/ui/SaveBar';
import type { SystemSettings } from '../api';

// NotificationSmtpSettingsProps 描述系统 SMTP 配置面板所需的状态和事件。
export interface NotificationSmtpSettingsProps {
  // smtp 是当前系统 SMTP 配置。
  smtp: SystemSettings;
  // setSmtp 更新 SMTP 配置字段。
  setSmtp: React.Dispatch<React.SetStateAction<SystemSettings>>;
  // smtpSaving 表示 SMTP 保存请求是否正在执行。
  smtpSaving: boolean;
  // showPassword 表示是否显示 SMTP 密码明文。
  showPassword: boolean;
  // setShowPassword 切换 SMTP 密码明文显示。
  setShowPassword: React.Dispatch<React.SetStateAction<boolean>>;
  // saveState 是区块保存条的四态；缺省时按 smtpSaving 推导为保存中或空闲。
  saveState?: SaveBarState;
  // errorMessage 是保存失败的具体原因，供保存条失败态展示。
  errorMessage?: string;
  // onSettled 是成功提示展示满后的复位回调。
  onSettled?: () => void;
  // onSave 保存 SMTP 配置。
  onSave: () => void | Promise<void>;
}

// NotificationSmtpSettings 渲染管理员可见的系统级 SMTP 设置。
export const NotificationSmtpSettings: React.FC<NotificationSmtpSettingsProps> = ({ smtp, setSmtp, smtpSaving, showPassword, setShowPassword, saveState, errorMessage, onSettled, onSave }) => {
  // resolvedSaveState 是保存条实际状态：优先使用页面传入的四态，未传入时退化为由 smtpSaving 推导的保存中/空闲。
  const resolvedSaveState: SaveBarState = saveState ?? (smtpSaving ? 'saving' : 'idle');
  // handleTextChange 更新 SMTP 文本字段。
  const handleTextChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    // key 是文本输入框数据属性中的 SMTP 字段名。
    const key = event.currentTarget.dataset.field as 'smtp_server' | 'smtp_user' | 'smtp_from_name' | 'smtp_from_address' | undefined;
    if (!key) return;
    setSmtp(
      // nextSettingsUpdater 使用函数式更新避免覆盖并发输入。
      current => ({ ...current, [key]: event.target.value }),
    );
  };
  // handlePortChange 更新 SMTP 端口并在非法输入时回退默认端口。
  const handlePortChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setSmtp(
      // nextSettingsUpdater 使用函数式更新端口字段。
      current => ({ ...current, smtp_port: parseInt(event.target.value, 10) || 587 }),
    );
  };
  // handleTlsChange 切换 STARTTLS 并保持 TLS 与 SSL 互斥。
  const handleTlsChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setSmtp(
      // nextSettingsUpdater 使用函数式更新 TLS/SSL 互斥字段。
      current => ({ ...current, smtp_use_tls: event.target.checked, smtp_use_ssl: event.target.checked ? false : current.smtp_use_ssl }),
    );
  };
  // handleSslChange 切换直连 SSL 并保持 TLS 与 SSL 互斥。
  const handleSslChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setSmtp(
      // nextSettingsUpdater 使用函数式更新 SSL/TLS 互斥字段。
      current => ({ ...current, smtp_use_ssl: event.target.checked, smtp_use_tls: event.target.checked ? false : current.smtp_use_tls }),
    );
  };
  // handlePasswordChange 更新系统 SMTP 密码。
  const handlePasswordChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setSmtp(
      // nextSettingsUpdater 使用函数式更新密码字段。
      current => ({ ...current, smtp_password: event.target.value }),
    );
  };
  // handleTogglePassword 切换系统 SMTP 密码明文展示。
  const handleTogglePassword = () => setShowPassword(
    // nextPasswordState 使用函数式更新密码显示状态。
    value => !value,
  );

  return (
    <section className="ios-card rounded-xl p-6 bg-white space-y-5">
      <div className="flex items-start justify-between">
        <div><h3 className="text-lg font-extrabold text-gray-800">SMTP 邮件配置</h3><p className="text-sm text-gray-500 mt-1">系统级邮件发送服务，供邮件通知渠道复用</p></div>
        <div className="p-2 rounded-xl bg-brand-50 text-brand-600"><Mail className="w-5 h-5" /></div>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">SMTP 服务器</label><input data-field="smtp_server" type="text" value={typeof smtp.smtp_server === 'string' ? smtp.smtp_server : ''} onChange={handleTextChange} placeholder="smtp.qq.com" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div>
        <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">SMTP 端口</label><input type="number" value={typeof smtp.smtp_port === 'number' ? smtp.smtp_port : 587} onChange={handlePortChange} placeholder="587" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div>
      </div>
      <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">发件邮箱</label><input data-field="smtp_user" type="email" value={typeof smtp.smtp_user === 'string' ? smtp.smtp_user : ''} onChange={handleTextChange} placeholder="your-email@qq.com" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div>
      <div className="space-y-2">
        <label className="block text-sm font-bold text-gray-800">邮箱密码 / 授权码</label>
        <div className="relative">
          <input type={showPassword ? 'text' : 'password'} value={typeof smtp.smtp_password === 'string' ? smtp.smtp_password : ''} onChange={handlePasswordChange} placeholder={smtp.smtp_password_configured ? '已配置，如需替换请输入新密码' : '输入密码或授权码'} className="w-full ios-input px-4 py-3 pr-12 rounded-xl text-sm" />
          <button type="button" onClick={handleTogglePassword} className="absolute right-3 top-1/2 -translate-y-1/2 p-2 text-gray-400 hover:text-gray-600 transition-colors">{showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}</button>
        </div>
        <p className="text-xs text-gray-500">QQ 邮箱需使用授权码（QQ 邮箱设置 → 账号 → 开启 SMTP → 生成授权码）</p>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">发件人显示名（可选）</label><input data-field="smtp_from_name" type="text" value={typeof smtp.smtp_from_name === 'string' ? smtp.smtp_from_name : ''} onChange={handleTextChange} placeholder="闲鱼自动回复系统" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div>
        <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">发件邮箱地址</label><input data-field="smtp_from_address" type="email" value={typeof smtp.smtp_from_address === 'string' ? smtp.smtp_from_address : (typeof smtp.smtp_user === 'string' ? smtp.smtp_user : '')} onChange={handleTextChange} placeholder="your-email@qq.com" className="w-full ios-input px-4 py-3 rounded-xl text-sm" /></div>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <label className="flex items-center gap-3 rounded-xl border border-gray-200 p-4 text-sm font-semibold text-gray-700"><input type="checkbox" checked={smtp.smtp_use_tls !== false} onChange={handleTlsChange} />STARTTLS（常用于 587 端口）</label>
        <label className="flex items-center gap-3 rounded-xl border border-gray-200 p-4 text-sm font-semibold text-gray-700"><input type="checkbox" checked={smtp.smtp_use_ssl === true} onChange={handleSslChange} />SSL/TLS 直连（常用于 465 端口）</label>
      </div>
      {/* 区块级保存条：SMTP 配置自成一份草稿，提交与成功/失败反馈都贴着本区块，不再让按钮孤立在表单末尾。 */}
      <SaveBar
        state={resolvedSaveState}
        onSave={/* 点击主按钮或失败态重试都重新提交同一份 SMTP 草稿。 */ () => void onSave()}
        label="保存 SMTP 配置"
        scopeLabel="修改发件服务参数后需保存"
        errorMessage={/* 使用 Hook 捕获的具体失败原因，未取到时回落到保存条默认文案。 */ errorMessage || undefined}
        successMessage="SMTP 配置已保存"
        onSettled={onSettled}
      />
    </section>
  );
};
