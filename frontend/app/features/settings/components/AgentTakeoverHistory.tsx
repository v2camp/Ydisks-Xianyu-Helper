import { Loader2,RefreshCw } from 'lucide-react';
import React,{ useCallback,useEffect,useRef,useState } from 'react';
import { formatLocalDateTime } from '../../../../dateTime';
import { getAgentTakeoverConversations,getAgentTakeoverMessages } from '../api';
import type { AgentConversationMessage,AgentTakeoverSession } from '../models';
import { isSettingsAbortError,settingsErrorMessage } from '../state';

/** LIST_PAGE_SIZE 是接管会话列表单页条数。 */
const LIST_PAGE_SIZE = 20;
/** MESSAGE_PAGE_SIZE 是单个会话消息单页条数。 */
const MESSAGE_PAGE_SIZE = 30;

/** TakeoverTarget 是消息读取所需的最小会话身份，避免把整个列表项带进请求。 */
interface TakeoverTarget {
  /** accountId 是会话所属账号标识。 */
  accountId: string;
  /** chatId 是平台会话标识。 */
  chatId: string;
}

/** SessionsCursor 是会话列表「加载更多」使用的游标，缺省表示从头读取。 */
interface SessionsCursor {
  /** beforeLastMessageAt 是上一页最后一条的最近消息毫秒时间。 */
  beforeLastMessageAt?: number;
  /** beforeChatId 是上一页最后一条的会话标识。 */
  beforeChatId?: string;
}

/** sessionKey 生成会话在列表中的稳定去重键。 */
const sessionKey = (session: TakeoverTarget): string => `${session.accountId}\u0000${session.chatId}`;

/** mergeSessions 合并两批会话并按账号与会话标识去重，保留先出现的顺序。 */
const mergeSessions = (current: AgentTakeoverSession[], incoming: AgentTakeoverSession[]): AgentTakeoverSession[] => {
  // merged 是按会话标识去重后的结果映射，Map 的插入顺序即展示顺序。
  const merged = new Map<string, AgentTakeoverSession>();
  for (const /* session 是当前待合并的会话摘要。 */ session of [...current, ...incoming]) {
    if (!merged.has(sessionKey(session))) merged.set(sessionKey(session), session);
  }
  return [...merged.values()];
};

/** mergeMessages 把新消息合并进当前消息流并按主键升序排列，保持时间顺序。 */
const mergeMessages = (current: AgentConversationMessage[], incoming: AgentConversationMessage[]): AgentConversationMessage[] => {
  // merged 是按消息主键去重后的结果映射，Map 的插入顺序后续会被主键排序覆盖。
  const merged = new Map<number, AgentConversationMessage>();
  for (const /* message 是当前待合并的单条消息。 */ message of [...current, ...incoming]) {
    if (!merged.has(message.id)) merged.set(message.id, message);
  }
  return [...merged.values()].sort(/* 排序回调按消息主键升序还原时间顺序。 */ (left, right) => left.id - right.id);
};

/** messagePlaceholder 把非文本消息归一为中文占位符，未知类型回落为带类型名的占位。 */
const messagePlaceholder = (messageType: string): string => {
  // normalizedType 是忽略大小写的消息类型，便于兼容后端不同写法。
  const normalizedType = messageType.toLowerCase();
  if (normalizedType === 'image') return '[图片]';
  if (normalizedType === 'audio' || normalizedType === 'voice') return '[语音]';
  if (normalizedType === 'video') return '[视频]';
  if (normalizedType === 'card') return '[卡片]';
  return `[${messageType}]`;
};

/** messageText 返回消息气泡实际展示的文本：纯文本原样显示，其余类型显示占位符。 */
const messageText = (message: AgentConversationMessage): string => (
  message.messageType && message.messageType !== 'text' ? messagePlaceholder(message.messageType) : message.content
);

/** AgentTakeoverHistory 渲染 AI 接管过的会话列表与只读消息回放；数据只读，不参与任何保存。 */
export const AgentTakeoverHistory: React.FC = () => {
  // sessions 保存按最近消息时间倒序的接管会话列表。
  const [sessions, setSessions] = useState<AgentTakeoverSession[]>([]);
  // sessionsLoading 表示会话列表是否正在读取。
  const [sessionsLoading, setSessionsLoading] = useState(false);
  // sessionsError 保存会话列表读取失败信息；空串表示无错误。
  const [sessionsError, setSessionsError] = useState('');
  // sessionsHasMore 表示是否还有更早的会话可分页加载。
  const [sessionsHasMore, setSessionsHasMore] = useState(false);
  // selected 是当前展开消息流的会话；null 表示尚未选择。
  const [selected, setSelected] = useState<AgentTakeoverSession | null>(null);
  // messages 保存当前会话按时间正序的只读消息流。
  const [messages, setMessages] = useState<AgentConversationMessage[]>([]);
  // messagesLoading 表示当前会话消息是否正在读取。
  const [messagesLoading, setMessagesLoading] = useState(false);
  // messagesError 保存消息读取失败信息；空串表示无错误。
  const [messagesError, setMessagesError] = useState('');
  // messagesHasMore 表示当前会话是否还有更早消息可分页加载。
  const [messagesHasMore, setMessagesHasMore] = useState(false);
  // sessionsController 保存当前可取消的会话列表请求。
  const sessionsController = useRef<AbortController | null>(null);
  // sessionsSequence 递增标记最新的会话列表读取代次，用于丢弃被后续加载取代的响应。
  const sessionsSequence = useRef(0);
  // sessionsCursor 保存会话列表下一次「加载更多」使用的游标。
  const sessionsCursor = useRef<SessionsCursor>({});
  // messagesController 保存当前可取消的消息读取请求。
  const messagesController = useRef<AbortController | null>(null);
  // messagesSequence 递增标记最新的消息读取代次，切换会话时据此丢弃旧响应。
  const messagesSequence = useRef(0);

  /** loadSessions 读取会话列表；append 为真时按游标追加下一页，否则从头替换。 */
  const loadSessions = useCallback(/* 当前回调封装会话列表的取消、代次与分页合并。 */ (append: boolean) => {
    sessionsController.current?.abort();
    // controller 是本次会话列表请求的取消控制器。
    const controller = new AbortController();
    sessionsController.current = controller;
    sessionsSequence.current += 1;
    // sequence 是本次会话列表读取的代次，晚到的旧响应会因代次不符被丢弃。
    const sequence = sessionsSequence.current;
    // cursor 是本次读取使用的游标；从头读取时为空。
    const cursor = append ? sessionsCursor.current : {};
    setSessionsLoading(true);
    setSessionsError('');
    void getAgentTakeoverConversations({
      limit: LIST_PAGE_SIZE,
      beforeLastMessageAt: cursor.beforeLastMessageAt,
      beforeChatId: cursor.beforeChatId,
    }, { signal: controller.signal })
      .then(/* response 是归一后的接管会话分页结果。 */ response => {
        if (sequence !== sessionsSequence.current || controller.signal.aborted) return;
        setSessions(/* updater 按追加或替换合并会话列表。 */ current => (append ? mergeSessions(current, response.sessions) : response.sessions));
        setSessionsHasMore(response.hasMore);
        sessionsCursor.current = {
          beforeLastMessageAt: response.nextBeforeLastMessageAt,
          beforeChatId: response.nextBeforeChatId,
        };
      })
      .catch(/* error 是会话列表读取失败或取消原因；取消不提示。 */ error => {
        if (sequence !== sessionsSequence.current || isSettingsAbortError(error)) return;
        setSessionsError(settingsErrorMessage(error, '读取接管会话失败'));
      })
      .finally(/* 当前回调在会话列表读取结束后收敛加载标记。 */ () => {
        if (sequence === sessionsSequence.current) setSessionsLoading(false);
      });
  }, []);

  /** loadMessages 读取指定会话的消息；beforeId 缺省表示读取最新一页。 */
  const loadMessages = useCallback(/* 当前回调封装消息读取的取消、代次与分页合并。 */ (target: TakeoverTarget, beforeId: number | undefined) => {
    messagesController.current?.abort();
    // controller 是本次消息请求的取消控制器。
    const controller = new AbortController();
    messagesController.current = controller;
    messagesSequence.current += 1;
    // sequence 是本次消息读取的代次，切换会话后旧响应会因代次不符被丢弃。
    const sequence = messagesSequence.current;
    setMessagesLoading(true);
    setMessagesError('');
    void getAgentTakeoverMessages({
      accountId: target.accountId,
      chatId: target.chatId,
      beforeId,
      limit: MESSAGE_PAGE_SIZE,
    }, { signal: controller.signal })
      .then(/* response 是归一后的聊天消息分页结果。 */ response => {
        if (sequence !== messagesSequence.current || controller.signal.aborted) return;
        setMessages(/* updater 合并最新一页或更早一页消息。 */ current => mergeMessages(current, response.messages));
        setMessagesHasMore(response.hasMore);
      })
      .catch(/* error 是消息读取失败或取消原因；取消不提示。 */ error => {
        if (sequence !== messagesSequence.current || isSettingsAbortError(error)) return;
        setMessagesError(settingsErrorMessage(error, '读取会话消息失败'));
      })
      .finally(/* 当前回调在消息读取结束后收敛加载标记。 */ () => {
        if (sequence === messagesSequence.current) setMessagesLoading(false);
      });
  }, []);

  // mountEffect 在挂载时读取首屏会话列表，并在卸载时中止全部在途请求。
  useEffect(/* effect 触发首屏读取，并返回卸载清理。 */ () => {
    loadSessions(false);
    return /* cleanup 在组件卸载时取消会话与消息请求。 */ () => {
      sessionsController.current?.abort();
      messagesController.current?.abort();
    };
  }, [loadSessions]);

  /** handleSelectSession 展开会话消息流：清空旧消息并读取该会话最新一页。 */
  const handleSelectSession = useCallback(/* 当前回调切换当前会话并清空旧消息。 */ (session: AgentTakeoverSession) => {
    setSelected(session);
    setMessages([]);
    setMessagesError('');
    setMessagesHasMore(false);
    loadMessages({ accountId: session.accountId, chatId: session.chatId }, undefined);
  }, [loadMessages]);

  /** handleLoadEarlier 从当前消息流第一条之前继续加载更早消息。 */
  const handleLoadEarlier = useCallback(/* 当前回调以当前首条消息主键为游标加载更早消息。 */ () => {
    if (!selected || messages.length === 0 || messagesLoading) return;
    loadMessages({ accountId: selected.accountId, chatId: selected.chatId }, messages[0].id);
  }, [loadMessages, messages, messagesLoading, selected]);

  /** handleMessagesRetry 重试当前会话消息读取：已有消息时续载更早一页，否则重读最新一页。 */
  const handleMessagesRetry = useCallback(/* 当前回调依据当前消息流状态选择重试方向。 */ () => {
    if (!selected) return;
    // hasExistingMessages 表示消息流中已有数据，此时失败多半发生在「加载更早」。
    const hasExistingMessages = messages.length > 0;
    loadMessages({ accountId: selected.accountId, chatId: selected.chatId }, hasExistingMessages ? messages[0].id : undefined);
  }, [loadMessages, messages, selected]);

  return (
    <div className="ios-card overflow-hidden rounded-xl bg-white" data-testid="agent-takeover-history">
      <div className="grid grid-cols-1 md:grid-cols-[280px_1fr]">
        {/* 会话列表：窄屏上下堆叠，桌面固定约 280px 左栏。 */}
        <aside className="flex min-h-0 flex-col border-b border-gray-100 md:border-b-0 md:border-r">
          <div className="flex items-center justify-between gap-2 px-4 py-3">
            <span className="text-sm font-bold text-gray-800">会话</span>
            <button
              type="button"
              onClick={/* 点击重新读取首屏会话列表。 */ () => loadSessions(false)}
              disabled={sessionsLoading}
              className="flex items-center gap-1 rounded-lg bg-gray-100 px-2.5 py-1.5 text-xs font-bold text-gray-700 hover:bg-gray-200 disabled:opacity-40"
            >
              <RefreshCw className={`h-3 w-3 ${sessionsLoading ? 'animate-spin' : ''}`} />
              刷新
            </button>
          </div>

          {sessionsError && (
            <div className="mx-3 mb-2 flex items-center justify-between gap-2 rounded-lg border border-danger-100 bg-danger-50 px-3 py-2 text-xs text-danger-700" role="alert">
              <span>{sessionsError}</span>
              <button type="button" className="font-bold underline" onClick={/* 点击重试首屏会话列表。 */ () => loadSessions(false)}>重新加载</button>
            </div>
          )}

          <div className="min-h-0 flex-1 overflow-y-auto">
            {sessionsLoading && sessions.length === 0 && !sessionsError && (
              <div className="flex items-center gap-2 px-4 py-6 text-sm text-gray-500" role="status">
                <Loader2 className="h-4 w-4 animate-spin" />
                加载接管会话中...
              </div>
            )}

            {!sessionsLoading && !sessionsError && sessions.length === 0 && (
              <div className="px-4 py-6 text-sm text-gray-400">暂无 AI 接管过的会话</div>
            )}

            <ul className="divide-y divide-gray-100">
              {sessions.map(/* session 是当前渲染的接管会话摘要。 */ session => {
                // active 表示该会话是否已被选中。
                const active = selected !== null && sessionKey(selected) === sessionKey(session);
                return (
                  <li key={sessionKey(session)}>
                    <button
                      type="button"
                      aria-pressed={active}
                      onClick={/* 点击切换当前会话并读取其最新消息。 */ () => handleSelectSession(session)}
                      className={`w-full px-4 py-3 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-300 ${active ? 'bg-brand-50' : 'hover:bg-gray-50'}`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="truncate text-sm font-bold text-gray-900">{session.peerName.trim() || '未知买家'}</span>
                        <span className="shrink-0 text-[10px] text-gray-500">{formatLocalDateTime(session.lastMessageAt)}</span>
                      </div>
                      <div className="mt-1 truncate text-xs text-gray-500">商品 {session.itemTitle.trim() || '未关联商品'}</div>
                      <div className="mt-0.5 flex items-center justify-between gap-2 text-[10px] text-gray-500">
                        <span className="truncate">账号 {session.accountId}</span>
                        <span className="shrink-0 font-bold text-brand">接管 {session.takeoverCount} 次</span>
                      </div>
                    </button>
                  </li>
                );
              })}
            </ul>

            {sessionsHasMore && (
              <div className="px-3 py-2">
                <button
                  type="button"
                  onClick={/* 点击按游标追加下一页会话。 */ () => loadSessions(true)}
                  disabled={sessionsLoading}
                  className="w-full rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold text-gray-700 hover:bg-gray-200 disabled:opacity-40"
                >
                  {sessionsLoading ? '加载中...' : '加载更多'}
                </button>
              </div>
            )}
          </div>
        </aside>

        {/* 消息区：只读回放；未选中会话时给出选择提示。 */}
        <section className="flex min-h-[26rem] flex-col">
          {!selected ? (
            <div className="flex flex-1 items-center justify-center px-6 py-10 text-sm text-gray-400">
              选择左侧会话查看消息
            </div>
          ) : (
            <>
              <div className="border-b border-gray-100 px-4 py-3">
                <div className="flex items-center justify-between gap-2">
                  <div className="truncate text-sm font-bold text-gray-800">
                    {selected.peerName.trim() || '未知买家'} · {selected.itemTitle.trim() || '未关联商品'}
                  </div>
                  <span className="shrink-0 text-xs text-gray-500">只读</span>
                </div>
                <p className="mt-0.5 text-xs text-gray-500">账号 {selected.accountId} · 最近消息 {formatLocalDateTime(selected.lastMessageAt)}</p>
              </div>

              {messagesHasMore && !messagesError && (
                <div className="px-4 pt-3">
                  <button
                    type="button"
                    onClick={handleLoadEarlier}
                    disabled={messagesLoading}
                    className="w-full rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold text-gray-700 hover:bg-gray-200 disabled:opacity-40"
                  >
                    {messagesLoading ? '加载中...' : '加载更早'}
                  </button>
                </div>
              )}

              {messagesError && (
                <div className="mx-4 mt-3 flex items-center justify-between gap-3 rounded-xl border border-danger-100 bg-danger-50 px-3 py-2 text-xs text-danger-700" role="alert">
                  <span>{messagesError}</span>
                  <button type="button" className="font-bold underline" onClick={handleMessagesRetry}>重试</button>
                </div>
              )}

              <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-4 py-3" data-testid="agent-takeover-messages">
                {messagesLoading && messages.length === 0 && !messagesError && (
                  <div className="flex items-center gap-2 text-sm text-gray-500" role="status">
                    <Loader2 className="h-4 w-4 animate-spin" />
                    加载消息中...
                  </div>
                )}

                {!messagesLoading && !messagesError && messages.length === 0 && (
                  <div className="text-sm text-gray-400">该会话暂无消息</div>
                )}

                {messages.map(/* message 是当前渲染的单条只读消息。 */ message => {
                  // outgoing 表示消息由卖家侧发出，展示在右侧；其余方向展示在左侧。
                  const outgoing = message.direction === 'outgoing';
                  return (
                    <div key={message.id} className={`flex ${outgoing ? 'justify-end' : 'justify-start'}`}>
                      <div className={`max-w-[80%] rounded-xl px-3 py-2 text-sm ${outgoing ? 'bg-brand-500 text-white' : 'bg-gray-100 text-gray-800'}`}>
                        <div className="whitespace-pre-wrap break-words">{messageText(message)}</div>
                        <div className={`mt-1 text-[10px] ${outgoing ? 'text-white/70' : 'text-gray-500'}`}>{formatLocalDateTime(message.sentAt)}</div>
                      </div>
                    </div>
                  );
                })}
              </div>
            </>
          )}
        </section>
      </div>
    </div>
  );
};

export default AgentTakeoverHistory;
