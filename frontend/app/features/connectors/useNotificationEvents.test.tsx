// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { NotificationEvent } from './api';
import { getNotificationEvents,getSystemNotificationEvents,markAllNotificationEventsRead,markNotificationEventRead } from './api';
import { useNotificationEvents } from './useNotificationEvents';

// 事件接口替身只控制站内通知事件请求，不访问网络。
vi.mock('./api', () => ({
  getNotificationEvents: vi.fn(),
  getSystemNotificationEvents: vi.fn(),
  markNotificationEventRead: vi.fn(),
  markAllNotificationEventsRead: vi.fn(),
  markSystemNotificationEventRead: vi.fn(),
  markAllSystemNotificationEventsRead: vi.fn(),
}));

// tenantEvent 是租户视图的单条未读事件。
const tenantEvent: NotificationEvent = { id: '1', scope: 'tenant', account_id: 'cid', event_type: 'account_offline', level: 'warn', title: '掉线', body: '正文', read: false, created_at: 1700000000 };
// systemEvent 是系统视图的单条未读事件。
const systemEvent: NotificationEvent = { id: '2', scope: 'system', event_type: 'business_silence', level: 'warn', title: '静默', body: '正文', read: false, created_at: 1700000001 };

// ScopeProps 描述作用域切换测试的 Hook 参数。
interface ScopeProps {
  /** scope 是当前通知作用域。 */
  scope: 'tenant' | 'system';
}

// EventPage 描述事件列表请求的待结算结果。
interface EventPage {
  /** items 是事件列表。 */
  items: NotificationEvent[];
  /** unread_count 是未读数。 */
  unread_count: number;
}

// deferred 构造可在测试中手动结算的 Promise。
const deferred = <T,>() => {
  // resolve、reject 是 Promise 的结算入口。
  let resolve!: (value: T) => void;
  // reject 是 Promise 的失败入口。
  let reject!: (reason?: unknown) => void;
  // promise 是待结算的 Promise。
  const promise = new Promise<T>(/* settle 是 Promise 结算回调。 */ (res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
};

describe('useNotificationEvents', /* 当前回调验证站内通知事件的加载、已读与作用域切换。 */ () => {
  beforeEach(/* 当前回调重置事件接口替身。 */ () => {
    vi.resetAllMocks();
    vi.mocked(getNotificationEvents).mockResolvedValue({ items: [tenantEvent], unread_count: 1 });
    vi.mocked(getSystemNotificationEvents).mockResolvedValue({ items: [systemEvent], unread_count: 1 });
    vi.mocked(markNotificationEventRead).mockResolvedValue({ success: true });
    vi.mocked(markAllNotificationEventsRead).mockResolvedValue({ success: true, updated: 1 });
  });

  test('加载租户事件后单条与全部已读都同步未读数', /* 当前回调验证租户事件已读状态。 */ async () => {
    // hook 是租户视图的渲染结果。
    const hook = renderHook(/* tenantScopeFactory 创建租户通知事件 Hook。 */ () => useNotificationEvents('tenant'));
    await waitFor(/* 等待初次加载结束。 */ () => expect(hook.result.current.loading).toBe(false));
    expect(hook.result.current.items).toHaveLength(1);
    expect(hook.result.current.unreadCount).toBe(1);
    await act(/* 标记单条事件已读。 */ async () => { await hook.result.current.markRead('1'); });
    expect(hook.result.current.items[0].read).toBe(true);
    expect(hook.result.current.unreadCount).toBe(0);
    await act(/* 重新加载后标记全部已读。 */ async () => {
      await hook.result.current.load();
      await hook.result.current.markAllRead();
    });
    expect(hook.result.current.items.every(/* item 是当前待判定的事件。 */ item => item.read)).toBe(true);
    expect(hook.result.current.unreadCount).toBe(0);
  });

  test('加载失败时暴露可读错误并结束加载态', /* 当前回调验证失败提示。 */ async () => {
    vi.mocked(getNotificationEvents).mockRejectedValue(new Error('网络异常'));
    // hook 是租户视图的渲染结果。
    const hook = renderHook(/* failingTenantFactory 创建租户通知事件 Hook。 */ () => useNotificationEvents('tenant'));
    await waitFor(/* 等待失败提示落定。 */ () => expect(hook.result.current.error).toBe('网络异常'));
    expect(hook.result.current.loading).toBe(false);
  });

  test('切换系统作用域后使用系统事件接口', /* 当前回调验证作用域切换。 */ async () => {
    // hook 保存可切换作用域的通知事件状态。
    const hook = renderHook(/* scopeFactory 按当前作用域创建 Hook。 */ ({ scope }: ScopeProps) => useNotificationEvents(scope), { initialProps: { scope: 'tenant' as 'tenant' | 'system' } });
    await waitFor(/* 等待租户事件加载完成。 */ () => expect(hook.result.current.loading).toBe(false));
    hook.rerender({ scope: 'system' });
    await waitFor(/* 等待系统事件加载完成。 */ () => expect(hook.result.current.items[0]?.id).toBe('2'));
    expect(getSystemNotificationEvents).toHaveBeenCalled();
  });

  test('旧加载响应不会覆盖新代次结果', /* 当前回调验证过期响应保护。 */ async () => {
    // first、second 是两次手动结算的列表请求。
    const first = deferred<EventPage>();
    // second 是第二次加载请求的待结算结果。
    const second = deferred<EventPage>();
    vi.mocked(getNotificationEvents).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    // hook 是租户视图的渲染结果。
    const hook = renderHook(/* staleFactory 创建租户通知事件 Hook。 */ () => useNotificationEvents('tenant'));
    await act(/* 触发第二次加载并先结算新结果。 */ async () => {
      void hook.result.current.load();
      second.resolve({ items: [{ ...tenantEvent, id: 'new' }], unread_count: 1 });
      await Promise.resolve();
    });
    await waitFor(/* 等待新结果生效。 */ () => expect(hook.result.current.items[0]?.id).toBe('new'));
    await act(/* 再结算旧响应。 */ async () => {
      first.resolve({ items: [{ ...tenantEvent, id: 'old' }], unread_count: 1 });
      await Promise.resolve();
    });
    expect(hook.result.current.items[0]?.id).toBe('new');
  });
});
