import { useCallback,useEffect,useRef,useState } from 'react';
import { getOpsConversations,getOpsMessages,sendOpsMessage } from './api';
import type { OpsConversation,OpsMessage } from './models';
import { isSettingsAbortError,settingsErrorMessage } from './state';

/** CONVERSATION_PAGE_SIZE 是运营会话列表单页条数。 */
const CONVERSATION_PAGE_SIZE = 20;
/** MESSAGE_PAGE_SIZE 是单个运营会话消息单页条数。 */
const MESSAGE_PAGE_SIZE = 30;

/** 运营 Agent 对话区的 Hook 返回值。 */
export interface UseOpsConversationsResult {
  /** conversations 是按最近消息时间倒序的运营会话列表。 */
  conversations: OpsConversation[];
  /** conversationsLoading 表示会话列表是否正在读取。 */
  conversationsLoading: boolean;
  /** conversationsError 保存会话列表读取失败信息；空串表示无错误。 */
  conversationsError: string;
  /** conversationsHasMore 表示是否还有下一页会话可加载。 */
  conversationsHasMore: boolean;
  /** refreshConversations 重新读取第一页会话。 */
  refreshConversations: () => void;
  /** loadMoreConversations 追加读取下一页会话。 */
  loadMoreConversations: () => void;
  /** selected 是当前选中的会话；null 表示尚未选择。 */
  selected: OpsConversation | null;
  /** selectConversation 切换当前会话并读取其最新消息。 */
  selectConversation: (conversation: OpsConversation) => void;
  /** messages 是当前会话按时间正序的消息流。 */
  messages: OpsMessage[];
  /** messagesLoading 表示当前会话消息是否正在读取。 */
  messagesLoading: boolean;
  /** messagesError 保存消息读取失败信息；空串表示无错误。 */
  messagesError: string;
  /** messagesHasMore 表示当前会话是否还有更早消息可分页加载。 */
  messagesHasMore: boolean;
  /** loadEarlierMessages 从当前消息流第一条之前继续加载更早消息。 */
  loadEarlierMessages: () => void;
  /** retryMessages 依据当前消息流状态重试失败的消息读取。 */
  retryMessages: () => void;
  /** sending 表示人工发送是否进行中；单飞期间不允许再次发送。 */
  sending: boolean;
  /** sendError 保存最近一次发送失败信息；空串表示无错误。 */
  sendError: string;
  /** send 发送一条人工文本，成功返回 true 并由组件清空输入。 */
  send: (content: string) => Promise<boolean>;
}

/** mergeConversations 合并两批会话并按主键去重，保留先出现的顺序。 */
const mergeConversations = (current: OpsConversation[], incoming: OpsConversation[]): OpsConversation[] => {
  // merged 是按会话主键去重后的结果映射，Map 的插入顺序即展示顺序。
  const merged = new Map<number, OpsConversation>();
  for (const /* conversation 是当前待合并的运营会话摘要。 */ conversation of [...current, ...incoming]) {
    if (!merged.has(conversation.id)) merged.set(conversation.id, conversation);
  }
  return [...merged.values()];
};

/** mergeMessages 合并两批消息并按主键去重，最终按主键升序排列以保持时间顺序。 */
const mergeMessages = (current: OpsMessage[], incoming: OpsMessage[]): OpsMessage[] => {
  // merged 是按消息主键去重后的结果映射，Map 的插入顺序后续会被主键排序覆盖。
  const merged = new Map<number, OpsMessage>();
  for (const /* message 是当前待合并的单条运营消息。 */ message of [...current, ...incoming]) {
    if (!merged.has(message.id)) merged.set(message.id, message);
  }
  return [...merged.values()].sort(/* 排序回调按消息主键升序还原时间顺序。 */ (left, right) => left.id - right.id);
};

/** 管理运营 Agent 对话区的会话分页、消息分页与人工发送；所有请求各自持有取消器与代次。 */
export const useOpsConversations = (): UseOpsConversationsResult => {
  // conversations 保存运营会话列表，按最近消息时间倒序。
  const [conversations, setConversations] = useState<OpsConversation[]>([]);
  // conversationsLoading 表示会话列表是否正在读取。
  const [conversationsLoading, setConversationsLoading] = useState(false);
  // conversationsError 保存会话列表读取失败信息。
  const [conversationsError, setConversationsError] = useState('');
  // conversationsHasMore 表示服务端是否还有下一页会话。
  const [conversationsHasMore, setConversationsHasMore] = useState(false);
  // conversationsPage 是会话列表当前已加载到的页码，从 1 开始。
  const [conversationsPage, setConversationsPage] = useState(1);
  // selected 是当前选中的运营会话；null 表示尚未选择。
  const [selected, setSelected] = useState<OpsConversation | null>(null);
  // messages 保存当前会话按时间正序的消息流。
  const [messages, setMessages] = useState<OpsMessage[]>([]);
  // messagesLoading 表示当前会话消息是否正在读取。
  const [messagesLoading, setMessagesLoading] = useState(false);
  // messagesError 保存消息读取失败信息。
  const [messagesError, setMessagesError] = useState('');
  // messagesHasMore 表示当前会话是否还有更早消息可分页加载。
  const [messagesHasMore, setMessagesHasMore] = useState(false);
  // sending 表示人工发送是否进行中，用于单飞与输入禁用。
  const [sending, setSending] = useState(false);
  // sendError 保存最近一次发送失败信息。
  const [sendError, setSendError] = useState('');
  // conversationsController 保存当前可取消的会话列表请求。
  const conversationsController = useRef<AbortController | null>(null);
  // conversationsSequence 递增标记最新的会话列表读取代次。
  const conversationsSequence = useRef(0);
  // messagesController 保存当前可取消的消息读取请求。
  const messagesController = useRef<AbortController | null>(null);
  // messagesSequence 递增标记最新的消息读取代次，切换会话时据此丢弃旧响应。
  const messagesSequence = useRef(0);
  // sendController 保存当前可取消的人工发送请求，卸载时中止。
  const sendController = useRef<AbortController | null>(null);
  // selectedRef 保存最新选中的会话，供发送响应到达时判断是否仍停留在原会话。
  const selectedRef = useRef<OpsConversation | null>(null);

  // selectedRef 在每次渲染后同步最新选中的会话，供异步回调读取。
  selectedRef.current = selected;

  /** loadConversations 读取会话列表；append 为真时读取指定页并追加，否则替换。 */
  const loadConversations = useCallback(/* 当前回调封装会话列表的取消、代次与分页合并。 */ (page: number, append: boolean) => {
    conversationsController.current?.abort();
    // controller 是本次会话列表请求的取消控制器。
    const controller = new AbortController();
    conversationsController.current = controller;
    conversationsSequence.current += 1;
    // sequence 是本次会话列表读取的代次，晚到的旧响应会因代次不符被丢弃。
    const sequence = conversationsSequence.current;
    setConversationsLoading(true);
    setConversationsError('');
    void getOpsConversations({ page, pageSize: CONVERSATION_PAGE_SIZE }, { signal: controller.signal })
      .then(/* response 是归一后的运营会话分页结果。 */ response => {
        if (sequence !== conversationsSequence.current || controller.signal.aborted) return;
        setConversations(/* updater 按追加或替换合并会话列表。 */ current => (append ? mergeConversations(current, response.conversations) : response.conversations));
        setConversationsPage(response.page);
        setConversationsHasMore(response.totalPages > response.page);
      })
      .catch(/* error 是会话列表读取失败或取消原因；取消不提示。 */ error => {
        if (sequence !== conversationsSequence.current || isSettingsAbortError(error)) return;
        setConversationsError(settingsErrorMessage(error, '读取运营会话失败'));
      })
      .finally(/* 当前回调在会话列表读取结束后收敛加载标记。 */ () => {
        if (sequence === conversationsSequence.current) setConversationsLoading(false);
      });
  }, []);

  /** refreshConversations 重新读取第一页会话，丢弃已累积的分页数据。 */
  const refreshConversations = useCallback(/* 当前回调把会话列表恢复到第一页。 */ () => loadConversations(1, false), [loadConversations]);

  /** loadMoreConversations 读取下一页会话并追加；没有更多或正在读取时忽略。 */
  const loadMoreConversations = useCallback(/* 当前回调按当前页码追加下一页。 */ () => {
    if (!conversationsHasMore || conversationsLoading) return;
    loadConversations(conversationsPage + 1, true);
  }, [conversationsHasMore, conversationsLoading, conversationsPage, loadConversations]);

  /** loadMessages 读取指定会话消息；beforeId 缺省表示读取最新一页。 */
  const loadMessages = useCallback(/* 当前回调封装消息读取的取消、代次与分页合并。 */ (conversationId: number, beforeId: number | undefined) => {
    messagesController.current?.abort();
    // controller 是本次消息请求的取消控制器。
    const controller = new AbortController();
    messagesController.current = controller;
    messagesSequence.current += 1;
    // sequence 是本次消息读取的代次，切换会话后旧响应会因代次不符被丢弃。
    const sequence = messagesSequence.current;
    setMessagesLoading(true);
    setMessagesError('');
    void getOpsMessages(conversationId, { beforeId, limit: MESSAGE_PAGE_SIZE }, { signal: controller.signal })
      .then(/* response 是归一后的运营消息分页结果。 */ response => {
        if (sequence !== messagesSequence.current || controller.signal.aborted) return;
        setMessages(/* updater 合并最新一页或更早一页消息，避免覆盖发送中已追加的出站消息。 */ current => mergeMessages(current, response.messages));
        setMessagesHasMore(response.hasMore);
      })
      .catch(/* error 是消息读取失败或取消原因；取消不提示。 */ error => {
        if (sequence !== messagesSequence.current || isSettingsAbortError(error)) return;
        setMessagesError(settingsErrorMessage(error, '读取运营消息失败'));
      })
      .finally(/* 当前回调在消息读取结束后收敛加载标记。 */ () => {
        if (sequence === messagesSequence.current) setMessagesLoading(false);
      });
  }, []);

  // mountEffect 在挂载时读取首屏会话列表，并在卸载时中止全部在途请求。
  useEffect(/* effect 触发首屏会话读取，并返回卸载清理。 */ () => {
    loadConversations(1, false);
    return /* cleanup 在组件卸载时取消会话、消息与发送请求。 */ () => {
      conversationsController.current?.abort();
      messagesController.current?.abort();
      sendController.current?.abort();
    };
  }, [loadConversations]);

  /** selectConversation 切换当前会话，清空旧消息并读取该会话最新一页。 */
  const selectConversation = useCallback(/* 当前回调切换选中会话并重置消息状态。 */ (conversation: OpsConversation) => {
    setSelected(conversation);
    setMessages([]);
    setMessagesError('');
    setMessagesHasMore(false);
    setSendError('');
    loadMessages(conversation.id, undefined);
  }, [loadMessages]);

  /** loadEarlierMessages 从当前消息流第一条之前继续加载更早消息。 */
  const loadEarlierMessages = useCallback(/* 当前回调以当前首条消息主键为游标加载更早消息。 */ () => {
    if (!selected || messages.length === 0 || messagesLoading) return;
    loadMessages(selected.id, messages[0].id);
  }, [loadMessages, messages, messagesLoading, selected]);

  /** retryMessages 重试当前会话消息读取：已有消息时续载更早一页，否则重读最新一页。 */
  const retryMessages = useCallback(/* 当前回调依据当前消息流状态选择重试方向。 */ () => {
    if (!selected) return;
    // hasExistingMessages 表示消息流中已有数据，此时失败多半发生在「加载更早」。
    const hasExistingMessages = messages.length > 0;
    loadMessages(selected.id, hasExistingMessages ? messages[0].id : undefined);
  }, [loadMessages, messages, selected]);

  /** send 向当前会话发送一条人工文本；成功时增量更新消息与列表摘要，不整表重拉。 */
  const send = useCallback(/* 当前回调封装人工发送的单飞、取消与增量更新。 */ async (content: string): Promise<boolean> => {
    // trimmed 是去除首尾空白后的待发送文本；空文本不产生请求。
    const trimmed = content.trim();
    if (!selected || sending || trimmed === '') return false;
    // target 是本次发送的会话快照，响应到达时用于判断用户是否已切换会话。
    const target = selected;
    sendController.current?.abort();
    // controller 是本次发送请求的取消控制器，组件卸载时会中止。
    const controller = new AbortController();
    sendController.current = controller;
    setSending(true);
    setSendError('');
    try {
      // result 是发送成功后的已落库消息与会话摘要。
      const result = await sendOpsMessage(target.id, trimmed, { signal: controller.signal });
      if (controller.signal.aborted) return false;
      // 会话列表按消息主键替换该项，保留原有顺序，不触发整表重拉。
      setConversations(/* updater 用返回的会话摘要替换对应列表项。 */ current => current.map(
        /* item 是当前列表项，命中目标会话时替换为最新摘要。 */ item => (item.id === result.conversation.id ? result.conversation : item),
      ));
      // 仍停留在原会话时才把新消息追加进消息流，避免切换会话后串到新会话里。
      if (selectedRef.current?.id === target.id) {
        setMessages(/* updater 追加服务端返回的出站消息并按主键去重。 */ current => mergeMessages(current, [result.message]));
        setSelected(/* updater 用返回的会话摘要刷新当前选中项，保持摘要与列表一致。 */ current => (current && current.id === result.conversation.id ? result.conversation : current));
      }
      return true;
    } catch (/* error 是发送失败或取消原因；取消不提示，真实失败留在页面上供重试。 */ error) {
      if (controller.signal.aborted || isSettingsAbortError(error)) return false;
      setSendError(settingsErrorMessage(error, '发送消息失败'));
      return false;
    } finally {
      if (!controller.signal.aborted) setSending(false);
    }
  }, [selected, sending]);

  return {
    conversations, conversationsLoading, conversationsError, conversationsHasMore,
    refreshConversations, loadMoreConversations,
    selected, selectConversation,
    messages, messagesLoading, messagesError, messagesHasMore, loadEarlierMessages, retryMessages,
    sending, sendError, send,
  };
};
