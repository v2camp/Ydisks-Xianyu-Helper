import { useCallback,useEffect,useRef,useState } from 'react';
import type { NotificationEvent } from './api';
import { getNotificationEvents,getSystemNotificationEvents,markAllNotificationEventsRead,markAllSystemNotificationEventsRead,markNotificationEventRead,markSystemNotificationEventRead } from './api';
import { notificationErrorMessage } from './state';

/** NotificationEventScope 是站内通知视图的作用域。 */
export type NotificationEventScope = 'tenant' | 'system';

/** NotificationEventsState 描述站内通知列表与已读动作的异步状态。 */
export interface NotificationEventsState {
  /** 当前视图可见的事件。 */
  items: NotificationEvent[];
  /** 当前视图未读事件数。 */
  unreadCount: number;
  /** 表示列表是否正在加载。 */
  loading: boolean;
  /** 保存最近一次加载或标记失败的可读提示。 */
  error: string | null;
  /** 表示已读动作是否正在执行。 */
  marking: boolean;
  /** load 重新读取当前视图事件。 */
  load: () => Promise<void>;
  /** markRead 标记单条事件已读。 */
  markRead: (eventID: string) => Promise<void>;
  /** markAllRead 标记当前视图全部事件已读。 */
  markAllRead: () => Promise<void>;
}

// useNotificationEvents 管理指定作用域的站内通知事件列表、未读数与已读动作。
export const useNotificationEvents = (scope: NotificationEventScope): NotificationEventsState => {
  // items 保存当前视图事件。
  const [items, setItems] = useState<NotificationEvent[]>([]);
  // unreadCount 保存当前视图未读数。
  const [unreadCount, setUnreadCount] = useState(0);
  // loading 表示列表是否正在加载。
  const [loading, setLoading] = useState(true);
  // error 保存最近一次失败提示。
  const [error, setError] = useState<string | null>(null);
  // marking 表示已读动作是否正在执行。
  const [marking, setMarking] = useState(false);
  // loadGeneration 隔离列表加载产生的旧响应。
  const loadGeneration = useRef(0);
  // loadAbort 保存当前列表请求的取消控制器。
  const loadAbort = useRef<AbortController | null>(null);
  // actionGeneration 隔离已读动作产生的旧响应。
  const actionGeneration = useRef(0);
  // actionAbort 保存当前已读动作的取消控制器。
  const actionAbort = useRef<AbortController | null>(null);

  // load 读取当前作用域的事件并丢弃过期响应。
  const load = useCallback(
    // 列表加载回调根据作用域选择租户或系统接口。
    async () => {
      // generation 标记本次查询代次。
      const generation = ++loadGeneration.current;
      loadAbort.current?.abort();
      // controller 允许刷新或卸载时取消列表请求。
      const controller = new AbortController();
      loadAbort.current = controller;
      setLoading(true);
      setError(null);
      try {
        // result 是当前作用域的事件列表与未读数。
        const result = await (scope === 'system'
          ? getSystemNotificationEvents(50, { signal: controller.signal })
          : getNotificationEvents(50, { signal: controller.signal }));
        if (generation !== loadGeneration.current || controller.signal.aborted) return;
        setItems(result.items);
        setUnreadCount(result.unread_count);
      } catch (loadError: unknown /* 列表加载异常 */) {
        if (generation === loadGeneration.current && !controller.signal.aborted) {
          setError(notificationErrorMessage(loadError, '读取通知事件失败'));
        }
      } finally {
        if (generation === loadGeneration.current) setLoading(false);
      }
    },
    [scope],
  );

  useEffect(
    // 作用域切换或挂载时加载事件，卸载时取消所有未完成请求。
    () => {
      void load();
      return (
        // cleanup 使旧代次失效并取消列表与已读请求。
        () => {
          loadGeneration.current += 1;
          loadAbort.current?.abort();
          actionGeneration.current += 1;
          actionAbort.current?.abort();
        }
      );
    },
    [load],
  );

  // markRead 标记单条事件已读并在本地同步未读数。
  const markRead = useCallback(
    // 单条已读回调按作用域调用对应接口。
    async (eventID: string) => {
      // generation 标记本次已读动作代次。
      const generation = ++actionGeneration.current;
      actionAbort.current?.abort();
      // controller 允许新动作取消旧请求。
      const controller = new AbortController();
      actionAbort.current = controller;
      setMarking(true);
      setError(null);
      try {
        await (scope === 'system'
          ? markSystemNotificationEventRead(eventID, { signal: controller.signal })
          : markNotificationEventRead(eventID, { signal: controller.signal }));
        if (generation !== actionGeneration.current || controller.signal.aborted) return;
        setItems(
          // nextItems 把对应事件标记为已读。
          current => current.map(
            // item 是当前待更新的事件。
            item => item.id === eventID ? { ...item, read: true } : item,
          ),
        );
        setUnreadCount(/* previous 是更新前未读数。 */ previous => Math.max(0, previous - 1));
      } catch (markError: unknown /* 单条已读异常 */) {
        if (generation === actionGeneration.current && !controller.signal.aborted) {
          setError(notificationErrorMessage(markError, '标记已读失败'));
        }
      } finally {
        if (generation === actionGeneration.current) setMarking(false);
      }
    },
    [scope],
  );

  // markAllRead 标记当前视图全部事件已读并清零未读数。
  const markAllRead = useCallback(
    // 全部已读回调按作用域调用对应接口。
    async () => {
      // generation 标记本次全部已读动作代次。
      const generation = ++actionGeneration.current;
      actionAbort.current?.abort();
      // controller 允许新动作取消旧请求。
      const controller = new AbortController();
      actionAbort.current = controller;
      setMarking(true);
      setError(null);
      try {
        await (scope === 'system'
          ? markAllSystemNotificationEventsRead({ signal: controller.signal })
          : markAllNotificationEventsRead({ signal: controller.signal }));
        if (generation !== actionGeneration.current || controller.signal.aborted) return;
        setItems(
          // nextItems 把全部事件标记为已读。
          current => current.map(
            // item 是当前待更新的事件。
            item => ({ ...item, read: true }),
          ),
        );
        setUnreadCount(0);
      } catch (markError: unknown /* 全部已读异常 */) {
        if (generation === actionGeneration.current && !controller.signal.aborted) {
          setError(notificationErrorMessage(markError, '标记全部已读失败'));
        }
      } finally {
        if (generation === actionGeneration.current) setMarking(false);
      }
    },
    [scope],
  );

  return { items, unreadCount, loading, error, marking, load, markRead, markAllRead };
};
