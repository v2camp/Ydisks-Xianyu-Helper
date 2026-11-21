import { Bell,Check,Loader2,Mail,PlugZap,Plus,RefreshCw,X } from 'lucide-react';
import React,{ useState } from 'react';
import { AsyncSection } from '../../../../shared/ui/AsyncSection';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { PageShell } from '../../../../shared/ui/PageShell';
import { SaveBar } from '../../../../shared/ui/SaveBar';
import { SectionHeader } from '../../../../shared/ui/SectionHeader';
import { ConnectorCard } from '../components/ConnectorCard';
import { UserAIConnectionCard } from '../components/UserAIConnectionCard';
import { NotificationChannelList } from '../components/NotificationChannelList';
import { NotificationChannelModal } from '../components/NotificationChannelModal';
import { NotificationSmtpSettings } from '../components/NotificationSmtpSettings';
import { useConnectors } from '../hooks';
import { connectorTypes } from '../registry';
import { useUserIntegrations } from '../../../../shared/async/useUserIntegrations';
import type { SystemSettings } from '../api';

// Connectors 渲染用户级「通知渠道」分区与「集成配置」分区，以及渠道编辑边界。
const Connectors: React.FC = () => {
  // connectorState 统一提供通知渠道的异步状态与动作。
  const connectorState = useConnectors();
  // integrations 提供当前用户的 AI、QQ 与 SMTP 集成配置草稿与保存动作。
  const integrations = useUserIntegrations();
  // showSmtpPassword 控制当前用户 SMTP 密码是否明文显示。
  const [showSmtpPassword, setShowSmtpPassword] = useState(false);
  // refreshing 表示页头「刷新」涉及的任一数据源仍在读取；此时按钮禁用并旋转，避免重复触发。
  const refreshing = connectorState.loading || integrations.loading;
  // handleIntegrationChange 把集成卡片提交的补丁合并进当前用户草稿。
  const handleIntegrationChange = (patch: SystemSettings) => {
    integrations.setSettings(/* previous 是当前集成配置草稿。 */ previous => ({ ...previous, ...patch }));
  };
  // refreshAll 同时刷新通知渠道与当前用户的集成配置。
  const refreshAll = async () => {
    await Promise.all([connectorState.loadChannels(), integrations.load()]);
  };

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
              onClick={/* 点击后并行重载通知渠道与集成配置。 */ () => void refreshAll()}
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

      {/* 分区顺序：用户先看到可直接操作的通知渠道，再配置支撑这些渠道与 AI 的用户级集成凭据。 */}
      <section className="space-y-4">
        {/* 通知渠道属于当前用户，普通用户无需滚动越过集成表单即可看到自己的渠道。 */}
        <SectionHeader
          icon={Bell}
          tone="brand"
          title="通知渠道"
          description="该用户的全部启用渠道对其全部闲鱼账号生效；单个账号可在「账号管理 → 编辑」里静音。"
        />

        <div className="ios-card rounded-xl p-5 bg-brand-50/50 border border-brand-100">
          <div className="flex items-start gap-3"><Bell className="w-5 h-5 text-brand mt-0.5 flex-shrink-0" /><div className="text-sm text-gray-700 leading-6">配置通知渠道后，以下事件会主动推送到该用户绑定的全部启用渠道；单个闲鱼账号可在账号编辑里静音。自动化任务通知已拆分为四类，可按需单独关闭：<ul className="mt-2 space-y-1 text-gray-600"><li>• <strong>拍下改价、付款发货、评价赠品、求评价</strong>：分别控制对应自动化任务结果</li><li>• <strong>账号 session 失效 / 自动恢复失败</strong>：账号状态告警</li><li>• <strong>触发风控验证</strong>：可能需要扫码完成验证</li></ul></div></div>
        </div>

        {connectorState.loading ? <div className="flex justify-center py-20"><Loader2 className="w-8 h-8 text-brand animate-spin" /></div> : <NotificationChannelList channels={connectorState.channels} testingId={connectorState.testingId} onEdit={connectorState.openEdit} onDelete={connectorState.handleDelete} onToggleEnabled={connectorState.handleToggleEnabled} onTest={connectorState.handleTest} />}
      </section>

      <section className="space-y-4">
        <SectionHeader
          icon={PlugZap}
          tone="slate"
          title="当前用户的集成配置"
          description="仅对当前本地用户生效；该用户的全部闲鱼账号共用这份 AI、QQ 与邮件配置。"
        />

        <AsyncSection
          loading={integrations.loading}
          error={integrations.loadError}
          onRetry={/* 点击重试重新读取当前用户的集成配置。 */ () => void integrations.load()}
          retryLabel="重新读取"
        >
          <>
            <div className="space-y-6">
              <UserAIConnectionCard
                settings={integrations.settings}
                onChange={handleIntegrationChange}
              />
              {connectorTypes.map(/* connector 是当前待渲染的连接器定义。 */ connector => (
                <ConnectorCard
                  key={connector.id}
                  connector={connector}
                  settings={integrations.settings}
                  loading={integrations.loading}
                  onChange={handleIntegrationChange}
                />
              ))}
            </div>

            <SaveBar
              className="mt-4"
              state={integrations.saveState}
              onSave={/* 点击主按钮或失败态重试都重新提交当前用户的集成配置。 */ () => void integrations.save()}
              label="保存集成配置"
              scopeLabel="修改 AI、QQ 或凭据后需保存"
              errorMessage={integrations.saveError || undefined}
              successMessage="集成配置已保存"
              sticky={false}
              onSettled={integrations.resetSaveState}
            />

            <section className="mt-8 space-y-4">
              <SectionHeader
                icon={Mail}
                tone="slate"
                title="邮件发送（SMTP）"
                description="当前用户的邮件渠道统一使用这份发件配置；不再支持渠道级独立 SMTP。"
              />
              <NotificationSmtpSettings
                smtp={integrations.settings}
                setSmtp={integrations.setSettings}
                smtpSaving={integrations.saveState === 'saving'}
                showPassword={showSmtpPassword}
                setShowPassword={setShowSmtpPassword}
                saveState={integrations.saveState}
                errorMessage={integrations.saveError}
                onSettled={integrations.resetSaveState}
                onSave={integrations.save}
              />
            </section>
          </>
        </AsyncSection>
      </section>

      <NotificationChannelModal showModal={connectorState.showModal} editing={connectorState.editing} form={connectorState.form} setForm={connectorState.setForm} saving={connectorState.saving} savedCredentialKeys={connectorState.savedCredentialKeys} resetCredentialState={connectorState.resetCredentialState} onClose={connectorState.closeModal} onSave={connectorState.handleSave} />

      {connectorState.toast && <div className={`fixed bottom-8 left-1/2 -translate-x-1/2 z-[10000] px-5 py-3 rounded-xl shadow-lg font-bold text-sm flex items-center gap-2 animate-fade-in text-white ${connectorState.toast.type === 'success' ? 'bg-success-500' : 'bg-danger-500'}`}>{connectorState.toast.type === 'success' ? <Check className="w-4 h-4" /> : <X className="w-4 h-4" />}{connectorState.toast.text}</div>}
    </PageShell>
  );
};

export default Connectors;
