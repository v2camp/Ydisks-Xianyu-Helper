import { Loader2,MessageSquare,RefreshCw,Send } from 'lucide-react';
import React,{ useCallback,useState } from 'react';
import { formatLocalDateTime } from '../../../../dateTime';
import { useOpsConversations } from '../opsHooks';
import type { OpsConversation,OpsMessage } from '../models';

/** MAX_MESSAGE_LENGTH 是与服务端一致的单条人工消息字符上限。 */
export const MAX_MESSAGE_LENGTH = 1500;

/** OpsConversationPanelProps 描述运营对话区的外部依赖状态。 */
export interface OpsConversationPanelProps {
  /** credentialsReady 表示 QQ 机器人凭据是否就绪；未就绪时禁用发送并提示去连接器页配置。 */
  credentialsReady: boolean;
}

/** conversationLabel 把会话目标归一为「群 · openid 前 8 位」/「用户 · openid 前 8 位」。 */
const conversationLabel = (conversation: OpsConversation): string => {
  // shortTarget 是目标 openid 的前 8 位，不足以识别时原样展示。
  const shortTarget = conversation.targetOpenid.slice(0, 8);
  return `${conversation.isGroup ? '群' : '用户'} · ${shortTarget || '未知目标'}`;
};

/** outboundSourceLabel 把出站消息来源归一为「命令回复」或「人工」。 */
const outboundSourceLabel = (message: OpsMessage): string => (message.source === 'operator' ? '人工' : '命令回复');

/** OpsConversationPanel 渲染运营 Agent 的会话列表、消息流与 Web 人工发送入口；手动刷新，不做轮询。 */
export const OpsConversationPanel: React.FC<OpsConversationPanelProps> = ({ credentialsReady }) => {
  // ops 是运营对话区的会话分页、消息分页与人工发送状态。
  const {
    conversations, conversationsLoading, conversationsError, conversationsHasMore,
    refreshConversations, loadMoreConversations,
    selected, selectConversation,
    messages, messagesLoading, messagesError, messagesHasMore, loadEarlierMessages, retryMessages,
    sending, sendError, send,
  } = useOpsConversations();
  // draft 是人工发送输入框的当前草稿；发送成功后清空，失败时保留原文。
  const [draft, setDraft] = useState('');

  /** handleSend 提交人工消息并把成功后的输入清空；失败时保留草稿供用户重试。 */
  const handleSend = useCallback(/* 当前回调封装发送成功清空与失败保留。 */ async () => {
    if (!credentialsReady || sending || !selected) return;
    // sent 表示本次发送是否已成功落库。
    const sent = await send(draft);
    if (sent) setDraft('');
  }, [credentialsReady, draft, selected, send, sending]);

  /** handleKeyDown 实现 Enter 发送、Shift+Enter 换行，并避开中文输入法的组合态。 */
  const handleKeyDown = useCallback(/* 当前回调把 Enter 绑定为发送快捷键。 */ (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void handleSend();
    }
  }, [handleSend]);

  return (
    <div className="ios-card overflow-hidden rounded-xl bg-white" data-testid="ops-conversation-panel">
      <div className="grid grid-cols-1 md:grid-cols-[280px_1fr]">
        {/* 会话列表：窄屏上下堆叠，桌面固定约 280px 左栏。 */}
        <aside className="flex min-h-0 flex-col border-b border-gray-100 md:border-b-0 md:border-r">
          <div className="flex items-center justify-between gap-2 px-4 py-3">
            <span className="text-sm font-bold text-gray-800">会话</span>
            <button
              type="button"
              onClick={refreshConversations}
              disabled={conversationsLoading}
              className="flex items-center gap-1 rounded-lg bg-gray-100 px-2.5 py-1.5 text-xs font-bold text-gray-700 hover:bg-gray-200 disabled:opacity-40"
            >
              <RefreshCw className={`h-3 w-3 ${conversationsLoading ? 'animate-spin' : ''}`} />
              刷新
            </button>
          </div>

          {conversationsError && (
            <div className="mx-3 mb-2 flex items-center justify-between gap-2 rounded-lg border border-danger-100 bg-danger-50 px-3 py-2 text-xs text-danger-700" role="alert">
              <span>{conversationsError}</span>
              <button type="button" className="font-bold underline" onClick={refreshConversations}>重新加载</button>
            </div>
          )}

          <div className="min-h-0 flex-1 overflow-y-auto">
            {conversationsLoading && conversations.length === 0 && !conversationsError && (
              <div className="flex items-center gap-2 px-4 py-6 text-sm text-gray-500" role="status">
                <Loader2 className="h-4 w-4 animate-spin" />
                加载会话中...
              </div>
            )}

            {!conversationsLoading && !conversationsError && conversations.length === 0 && (
              <div className="px-4 py-6 text-sm text-gray-400">暂无会话</div>
            )}

            <ul className="divide-y divide-gray-100">
              {conversations.map(/* conversation 是当前渲染的运营会话摘要。 */ conversation => {
                // active 表示该会话是否已被选中。
                const active = selected?.id === conversation.id;
                return (
                  <li key={conversation.id}>
                    <button
                      type="button"
                      aria-pressed={active}
                      onClick={/* 点击切换当前会话并读取其最新消息。 */ () => selectConversation(conversation)}
                      className={`w-full px-4 py-3 text-left transition-colors ${active ? 'bg-brand-50' : 'hover:bg-gray-50'}`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-bold text-gray-900">{conversationLabel(conversation)}</span>
                        <span className="shrink-0 text-[10px] text-gray-500">{formatLocalDateTime(conversation.lastMessageAt)}</span>
                      </div>
                      <div className="mt-1 truncate text-xs text-gray-500">{conversation.lastMessagePreview || '暂无消息'}</div>
                    </button>
                  </li>
                );
              })}
            </ul>

            {conversationsHasMore && (
              <div className="px-3 py-2">
                <button
                  type="button"
                  onClick={loadMoreConversations}
                  disabled={conversationsLoading}
                  className="w-full rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold text-gray-700 hover:bg-gray-200 disabled:opacity-40"
                >
                  {conversationsLoading ? '加载中...' : '加载更多'}
                </button>
              </div>
            )}
          </div>
        </aside>

        {/* 消息与输入：未选中会话时只展示占位说明。 */}
        <section className="flex min-h-[26rem] flex-col">
          {!selected ? (
            <div className="flex flex-1 items-center justify-center px-6 py-10 text-sm text-gray-400">
              选择左侧会话查看消息
            </div>
          ) : (
            <>
              <div className="border-b border-gray-100 px-4 py-3">
                <div className="text-sm font-bold text-gray-800">{conversationLabel(selected)}</div>
                <p className="mt-0.5 text-xs text-gray-500">最近消息 {formatLocalDateTime(selected.lastMessageAt)}</p>
              </div>

              {messagesHasMore && !messagesError && (
                <div className="px-4 pt-3">
                  <button
                    type="button"
                    onClick={loadEarlierMessages}
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
                  <button type="button" className="font-bold underline" onClick={retryMessages}>重试</button>
                </div>
              )}

              <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-4 py-3" data-testid="ops-messages">
                {messagesLoading && messages.length === 0 && !messagesError && (
                  <div className="flex items-center gap-2 text-sm text-gray-500" role="status">
                    <Loader2 className="h-4 w-4 animate-spin" />
                    加载消息中...
                  </div>
                )}

                {!messagesLoading && !messagesError && messages.length === 0 && (
                  <div className="text-sm text-gray-400">暂无消息</div>
                )}

                {messages.map(/* message 是当前渲染的单条运营消息。 */ message => {
                  // outbound 表示消息由本系统发出，展示在右侧；入站消息展示在左侧。
                  const outbound = message.direction === 'outbound';
                  return (
                    <div key={message.id} className={`flex ${outbound ? 'justify-end' : 'justify-start'}`}>
                      <div className={`max-w-[80%] rounded-xl px-3 py-2 text-sm ${outbound ? 'bg-brand-500 text-white' : 'bg-gray-100 text-gray-800'}`}>
                        <div className="whitespace-pre-wrap break-words">{message.content}</div>
                        <div className={`mt-1 text-[10px] ${outbound ? 'text-white/70' : 'text-gray-500'}`}>
                          {outbound && <span className="mr-2 font-bold">{outboundSourceLabel(message)}</span>}
                          {formatLocalDateTime(message.createdAt)}
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>

              <div className="border-t border-gray-100 px-4 py-3">
                {!credentialsReady && (
                  <p className="mb-2 rounded-lg bg-warning-50 px-3 py-2 text-xs text-warning-800">先在连接器页配置 QQ 机器人</p>
                )}
                {sendError && <p className="mb-2 text-xs text-danger-600">{sendError}</p>}
                <div className="flex items-end gap-2">
                  <textarea
                    aria-label="发送运营消息"
                    value={draft}
                    maxLength={MAX_MESSAGE_LENGTH}
                    onChange={/* 输入变化更新草稿并强制不超过字符上限。 */ event => setDraft(event.target.value.slice(0, MAX_MESSAGE_LENGTH))}
                    onKeyDown={handleKeyDown}
                    disabled={!credentialsReady || sending}
                    placeholder="输入消息，Enter 发送，Shift+Enter 换行"
                    rows={2}
                    className="min-h-[3rem] w-full resize-y rounded-xl border border-gray-200 px-3 py-2 text-sm focus:border-brand focus:outline-none disabled:bg-gray-50 disabled:text-gray-400"
                  />
                  <button
                    type="button"
                    onClick={/* 点击发送当前草稿。 */ () => void handleSend()}
                    disabled={!credentialsReady || sending || draft.trim() === ''}
                    className="flex shrink-0 items-center gap-1 rounded-xl bg-brand px-4 py-3 text-sm font-bold text-white hover:bg-brand-highlight disabled:opacity-40"
                  >
                    {sending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
                    发送
                  </button>
                </div>
                <div className="mt-1 flex items-center justify-between text-[10px] text-gray-500">
                  <span className="inline-flex items-center gap-1"><MessageSquare className="h-3 w-3" />纯文本，不解析 Markdown</span>
                  <span>{draft.length}/{MAX_MESSAGE_LENGTH}</span>
                </div>
              </div>
            </>
          )}
        </section>
      </div>
    </div>
  );
};

export default OpsConversationPanel;
