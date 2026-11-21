import { Bell,CheckCheck,Loader2,RefreshCw } from 'lucide-react';
import React from 'react';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { PageShell } from '../../../../shared/ui/PageShell';
import { SectionHeader } from '../../../../shared/ui/SectionHeader';
import { NotificationEventList } from '../components/NotificationEventList';
import { useNotificationEvents } from '../useNotificationEvents';

// NotificationCenter 展示当前用户的租户站内通知事件、未读状态与已读动作。
const NotificationCenter: React.FC = () => {
  // events 提供当前用户的租户事件列表与已读动作。
  const events = useNotificationEvents('tenant');

  return (
    <PageShell>
      <PageHeader
        icon={Bell}
        title="通知中心"
        subtitle="当前用户的闲鱼账号事件；外发渠道在连接器页配置"
        actions={(
          <button
            type="button"
            onClick={/* 点击后重读当前用户事件。 */ () => void events.load()}
            disabled={events.loading}
            className="flex items-center gap-2 rounded-xl bg-gray-100 px-4 py-2 font-bold text-gray-700 transition-colors hover:bg-gray-200 disabled:cursor-not-allowed disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4${events.loading ? ' animate-spin' : ''}`} />
            刷新
          </button>
        )}
      />

      <section className="space-y-4">
        <SectionHeader
          icon={Bell}
          tone="brand"
          title="最近事件"
          description="站内历史不受账号静音影响；静音只停止外部渠道推送。"
        />

        <div className="flex flex-wrap items-center justify-between gap-3">
          <span className="text-sm text-gray-600">未读 {events.unreadCount} 条</span>
          <button
            type="button"
            onClick={/* 点击后标记全部事件已读。 */ () => void events.markAllRead()}
            disabled={events.marking || events.unreadCount === 0}
            className="inline-flex items-center gap-2 rounded-xl bg-brand px-4 py-2 text-sm font-bold text-white transition-opacity disabled:cursor-not-allowed disabled:opacity-50"
          >
            <CheckCheck className="h-4 w-4" />
            全部标记已读
          </button>
        </div>

        {events.error && <div className="rounded-xl bg-danger-50 px-4 py-3 text-sm text-danger-700">{events.error}</div>}

        {events.loading ? (
          <div className="flex justify-center py-20"><Loader2 className="h-8 w-8 animate-spin text-brand" /></div>
        ) : (
          <NotificationEventList
            items={events.items}
            marking={events.marking}
            onMarkRead={/* 单条事件已读入口。 */ eventID => void events.markRead(eventID)}
            emptyText="暂无通知事件"
          />
        )}
      </section>
    </PageShell>
  );
};

export default NotificationCenter;
