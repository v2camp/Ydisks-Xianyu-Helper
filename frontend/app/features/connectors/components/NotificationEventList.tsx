import { CheckCheck } from 'lucide-react';
import React from 'react';
import type { NotificationEvent } from '../api';
import { notificationEventLabel, notificationEventTime, notificationLevelLabel } from '../state';

/** NotificationEventListProps 描述站内通知事件列表的展示与已读动作。 */
export interface NotificationEventListProps {
  /** items 是当前视图事件。 */
  items: NotificationEvent[];
  /** marking 表示已读动作是否正在执行。 */
  marking: boolean;
  /** onMarkRead 标记单条事件已读。 */
  onMarkRead: (eventID: string) => void;
  /** emptyText 是列表为空时的提示文案。 */
  emptyText: string;
}

// levelTone 返回事件级别的视觉色。
const levelTone = (level: string): string => {
  if (level === 'critical') return 'bg-danger-100 text-danger-700';
  if (level === 'warn') return 'bg-warning-100 text-warning-700';
  return 'bg-brand-50 text-brand-700';
};

// NotificationEventList 渲染站内通知事件卡片与单条已读入口。
export const NotificationEventList: React.FC<NotificationEventListProps> = ({ items, marking, onMarkRead, emptyText }) => {
  if (items.length === 0) {
    return <div className="ios-card rounded-xl p-10 text-center text-sm text-gray-500">{emptyText}</div>;
  }
  return (
    <ul className="space-y-3">
      {items.map(
        // item 是当前待渲染的事件。
        item => (
          <li key={item.id} className={`ios-card rounded-xl p-5 transition-opacity ${item.read ? 'opacity-70' : ''}`}>
            <div className="flex flex-wrap items-center gap-2">
              <span className={`rounded-full px-2.5 py-1 text-xs font-bold ${levelTone(item.level)}`}>{notificationLevelLabel(item.level)}</span>
              <span className="text-xs text-gray-500">{notificationEventTime(item.created_at)}</span>
              {item.account_id && <span className="text-xs text-gray-500">账号: {item.account_id}</span>}
              {!item.read && <span className="h-2 w-2 rounded-full bg-brand" aria-label="未读" />}
            </div>
            <h3 className="mt-3 text-base font-bold text-gray-900">{item.title || notificationEventLabel(item.event_type)}</h3>
            <p className="mt-1 text-xs text-gray-500">{notificationEventLabel(item.event_type)}</p>
            {item.body && <p className="mt-3 whitespace-pre-wrap break-words text-sm leading-6 text-gray-700">{item.body}</p>}
            {!item.read && (
              <button
                type="button"
                disabled={marking}
                onClick={/* 点击后标记当前事件已读。 */ () => onMarkRead(item.id)}
                className="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold text-gray-700 transition-colors hover:bg-gray-200 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <CheckCheck className="h-3.5 w-3.5" />
                标记已读
              </button>
            )}
          </li>
        ),
      )}
    </ul>
  );
};
