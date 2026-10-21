import { Bell,Check,Loader2,PlugZap,Plus,RefreshCw,X } from 'lucide-react';
import React from 'react';
import { AsyncSection } from '../../../../shared/ui/AsyncSection';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { PageShell } from '../../../../shared/ui/PageShell';
import { SaveBar } from '../../../../shared/ui/SaveBar';
import { SectionHeader } from '../../../../shared/ui/SectionHeader';
import { ConnectorCard } from '../components/ConnectorCard';
import { NotificationChannelList } from '../components/NotificationChannelList';
import { NotificationChannelModal } from '../components/NotificationChannelModal';
import { NotificationSmtpSettings } from '../components/NotificationSmtpSettings';
import { useConnectors } from '../hooks';
import { connectorTypes } from '../registry';

// ConnectorsProps 描述连接器页面接收的权限上下文。
interface ConnectorsProps {
  // isAdmin 表示当前用户是否可以查看和保存系统级连接器与 SMTP 配置。
  isAdmin?: boolean;
}

// Connectors 渲染平台连接器、通知渠道列表、SMTP 配置和渠道编辑边界。
const Connectors: React.FC<ConnectorsProps> = ({ isAdmin = false }) => {
  // connectorState 统一提供连接器页面的数据、表单和异步动作。
  const connectorState = useConnectors(isAdmin);
  // refreshing 表示页头「刷新」涉及的任一数据源仍在读取；此时按钮禁用并旋转，避免重复触发。
  const refreshing = connectorState.loading || connectorState.settingsLoading;

  return (
    <PageShell>
      <PageHeader
        icon={PlugZap}
        title="连接器"
        subtitle="配置第三方平台连接器与通知渠道，账号异常时主动推送告警"
        actions={(
          <>
            {/* 刷新对管理员是「渠道 + 系统设置」的整体重载，普通用户只刷新自己的渠道列表。 */}
            <button
              type="button"
              onClick={/* 点击后按权限重载对应数据源。 */ () => void connectorState.refresh()}
              disabled={refreshing}
              className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <RefreshCw className={`w-4 h-4${refreshing ? ' animate-spin' : ''}`} />
              刷新
            </button>
            <button onClick={connectorState.openCreate} className="ios-btn-primary px-5 py-2.5 rounded-xl font-bold flex items-center gap-2"><Plus className="w-4 h-4" />新建渠道</button>
          </>
        )}
      />

      {isAdmin && (
        <section className="space-y-4">
          <SectionHeader
            icon={PlugZap}
            title="平台连接器"
            description="系统级平台凭据，保存后由通知渠道继承复用；只需配置一次，无需逐条重复填写。"
            tone="indigo"
          />

          {/* 区块级异步容器：系统设置读取失败时必须在界面可见，并给出重新读取入口，不能让卡片停在空草稿上。 */}
          <AsyncSection
            loading={connectorState.settingsLoading}
            error={connectorState.settingsError}
            onRetry={/* 点击重试重新读取系统设置。 */ () => void connectorState.loadSmtp()}
            retryLabel="重新读取"
          >
            <>
              <div className="space-y-6">
                {connectorTypes.map(/* connector 是当前待渲染的连接器定义。 */ connector => (
                  <ConnectorCard
                    key={connector.id}
                    connector={connector}
                    settings={connectorState.connectorSettings}
                    loading={connectorState.settingsLoading}
                    onChange={/* patch 合并进连接器配置草稿，由下方保存条统一提交。 */ patch => connectorState.setConnectorSettings(/* previous 是合并补丁前的连接器草稿。 */ previous => ({ ...previous, ...patch }))}
                  />
                ))}
              </div>

              {/* 区块级保存条：连接器凭据与能力开关共用一份草稿，提交与反馈都贴着本区块。 */}
              <SaveBar
                className="mt-4"
                state={connectorState.connectorSaveState}
                onSave={/* 点击主按钮或失败态重试都重新提交同一份连接器草稿。 */ () => void connectorState.handleSaveConnector()}
                label="保存连接器配置"
                scopeLabel="修改平台凭据或能力开关后需保存"
                errorMessage={connectorState.connectorSaveError || undefined}
                successMessage="连接器配置已保存"
                sticky={false}
                onSettled={connectorState.resetConnectorSaveState}
              />
            </>
          </AsyncSection>
        </section>
      )}

      <div className="ios-card rounded-xl p-5 bg-blue-50/50 border border-blue-100">
        <div className="flex items-start gap-3"><Bell className="w-5 h-5 text-brand mt-0.5 flex-shrink-0" /><div className="text-sm text-gray-700 leading-6">配置通知渠道并在「账号管理 → 编辑」里绑定后，以下事件会主动推送到该账号绑定的渠道；自动化任务通知已拆分为四类，可按需单独关闭：<ul className="mt-2 space-y-1 text-gray-600"><li>• <strong>拍下改价、付款发货、评价赠品、求评价</strong>：分别控制对应自动化任务结果</li><li>• <strong>账号 session 失效 / 自动恢复失败</strong>：账号状态告警</li><li>• <strong>触发风控验证</strong>：可能需要扫码完成验证</li></ul></div></div>
      </div>

      {connectorState.loading ? <div className="flex justify-center py-20"><Loader2 className="w-8 h-8 text-brand animate-spin" /></div> : <NotificationChannelList channels={connectorState.channels} testingId={connectorState.testingId} onEdit={connectorState.openEdit} onDelete={connectorState.handleDelete} onToggleEnabled={connectorState.handleToggleEnabled} onTest={connectorState.handleTest} />}

      {isAdmin && (
        /* SMTP 与连接器共用同一次系统设置读取，失败同样走可见的错误态而不是静默留空。 */
        <AsyncSection
          loading={connectorState.settingsLoading}
          error={connectorState.settingsError}
          onRetry={/* 点击重试重新读取系统设置。 */ () => void connectorState.loadSmtp()}
          retryLabel="重新读取"
        >
          <NotificationSmtpSettings
            smtp={connectorState.smtp}
            setSmtp={connectorState.setSmtp}
            smtpSaving={connectorState.smtpSaving}
            showPassword={connectorState.showSmtpPassword}
            setShowPassword={connectorState.setShowSmtpPassword}
            saveState={connectorState.smtpSaveState}
            errorMessage={connectorState.smtpSaveError}
            onSettled={connectorState.resetSmtpSaveState}
            onSave={connectorState.handleSaveSmtp}
          />
        </AsyncSection>
      )}

      <NotificationChannelModal showModal={connectorState.showModal} editing={connectorState.editing} form={connectorState.form} setForm={connectorState.setForm} smtp={connectorState.smtp} showChannelSmtpPassword={connectorState.showChannelSmtpPassword} setShowChannelSmtpPassword={connectorState.setShowChannelSmtpPassword} saving={connectorState.saving} onClose={connectorState.closeModal} onSave={connectorState.handleSave} />

      {connectorState.toast && <div className={`fixed bottom-8 left-1/2 -translate-x-1/2 z-[10000] px-5 py-3 rounded-xl shadow-lg font-bold text-sm flex items-center gap-2 animate-fade-in text-white ${connectorState.toast.type === 'success' ? 'bg-success-500' : 'bg-danger-500'}`}>{connectorState.toast.type === 'success' ? <Check className="w-4 h-4" /> : <X className="w-4 h-4" />}{connectorState.toast.text}</div>}
    </PageShell>
  );
};

export default Connectors;
