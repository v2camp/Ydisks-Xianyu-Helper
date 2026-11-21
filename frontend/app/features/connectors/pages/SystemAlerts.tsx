import { Bell,Check,CheckCheck,Loader2,Plus,RefreshCw,ShieldAlert,X } from 'lucide-react';
import React from 'react';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { PageShell } from '../../../../shared/ui/PageShell';
import { SectionHeader } from '../../../../shared/ui/SectionHeader';
import { NotificationChannelList } from '../components/NotificationChannelList';
import { NotificationChannelModal } from '../components/NotificationChannelModal';
import { NotificationEventList } from '../components/NotificationEventList';
import { useSystemAlertChannels } from '../hooks';
import { systemAlertEvents } from '../state';
import { useNotificationEvents } from '../useNotificationEvents';

// SystemAlerts 展示实例级系统告警事件与管理员共享的系统告警渠道。
const SystemAlerts: React.FC = () => {
  // events 提供系统级事件列表与已读动作。
  const events = useNotificationEvents('system');
  // channels 提供系统告警渠道的列表、编辑弹窗与渠道动作。
  const channels = useSystemAlertChannels();
  // refreshAll 同时刷新系统事件与系统告警渠道。
  const refreshAll = async () => {
    await Promise.all([events.load(), channels.loadChannels()]);
  };
  // refreshing 表示任一数据源仍在读取。
  const refreshing = events.loading || channels.loading;

  return (
    <PageShell>
      <PageHeader
        icon={ShieldAlert}
        title="系统告警"
        subtitle="实例级告警与管理员接收渠道；与租户通知渠道隔离"
        actions={(
          <button
            type="button"
            onClick={/* 点击后并行重载系统事件与渠道。 */ () => void refreshAll()}
            disabled={refreshing}
            className="flex items-center gap-2 rounded-xl bg-gray-100 px-4 py-2 font-bold text-gray-700 transition-colors hover:bg-gray-200 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4${refreshing ? ' animate-spin' : ''}`} />
            刷新
          </button>
        )}
      />

      <section className="space-y-4">
        <SectionHeader
          icon={ShieldAlert}
          tone="slate"
          title="系统事件"
          description="进程与实例级告警；不归属任何租户账号。"
        />
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span className="text-sm text-gray-600">未读 {events.unreadCount} 条</span>
          <button
            type="button"
            onClick={/* 点击后标记全部系统事件已读。 */ () => void events.markAllRead()}
            disabled={events.marking || events.unreadCount === 0}
            className="inline-flex items-center gap-2 rounded-xl bg-brand px-4 py-2 text-sm font-bold text-white transition-opacity disabled:cursor-not-allowed disabled:opacity-50"
          >
            <CheckCheck className="h-4 w-4" />
            全部标记已读
          </button>
        </div>
        {events.error && <div className="rounded-xl bg-danger-50 px-4 py-3 text-sm text-danger-700">{events.error}</div>}
        {events.loading ? (
          <div className="flex justify-center py-12"><Loader2 className="h-8 w-8 animate-spin text-brand" /></div>
        ) : (
          <NotificationEventList
            items={events.items}
            marking={events.marking}
            onMarkRead={/* 单条系统事件已读入口。 */ eventID => void events.markRead(eventID)}
            emptyText="暂无系统告警"
          />
        )}
      </section>

      <section className="mt-10 space-y-4">
        <SectionHeader
          icon={Bell}
          tone="slate"
          title="系统告警渠道"
          description="仅管理员可见；只订阅实例级事件，不影响租户通知渠道。"
        />
        <div className="flex justify-end">
          <button type="button" onClick={channels.openCreate} className="ios-btn-primary flex items-center gap-2 rounded-xl px-5 py-2.5 font-bold">
            <Plus className="h-4 w-4" />
            新建渠道
          </button>
        </div>
        {channels.loading ? (
          <div className="flex justify-center py-12"><Loader2 className="h-8 w-8 animate-spin text-brand" /></div>
        ) : (
          <NotificationChannelList
            channels={channels.channels}
            testingId={channels.testingId}
            onEdit={channels.openEdit}
            onDelete={channels.handleDelete}
            onToggleEnabled={channels.handleToggleEnabled}
            onTest={channels.handleTest}
          />
        )}
      </section>

      <NotificationChannelModal
        showModal={channels.showModal}
        editing={channels.editing}
        form={channels.form}
        setForm={channels.setForm}
        saving={channels.saving}
        savedCredentialKeys={channels.savedCredentialKeys}
        resetCredentialState={channels.resetCredentialState}
        onClose={channels.closeModal}
        onSave={channels.handleSave}
        events={systemAlertEvents}
      />

      {channels.toast && (
        <div className={`fixed bottom-8 left-1/2 z-[10000] flex -translate-x-1/2 items-center gap-2 rounded-xl px-5 py-3 text-sm font-bold text-white shadow-lg animate-fade-in ${channels.toast.type === 'success' ? 'bg-success-500' : 'bg-danger-500'}`}>
          {channels.toast.type === 'success' ? <Check className="h-4 w-4" /> : <X className="h-4 w-4" />}
          {channels.toast.text}
        </div>
      )}
    </PageShell>
  );
};

export default SystemAlerts;
