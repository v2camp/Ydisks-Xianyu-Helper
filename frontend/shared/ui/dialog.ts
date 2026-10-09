// dialog 模块把浏览器原生 alert/confirm/prompt 升级为页面居中的模态对话框。
// 业务代码在事件处理器或 Hook 中调用 appDialog，宿主未挂载时自动降级为原生弹框。

// DialogVariant 描述对话框的语义配色，决定图标、徽章与主按钮的样式。
export type DialogVariant = 'info' | 'success' | 'warning' | 'error' | 'question' | 'danger';

// DialogKind 描述对话框的交互类型，决定返回值与按钮数量。
export type DialogKind = 'alert' | 'confirm' | 'prompt';

// DialogOptions 描述单次对话框请求的展示定制项，未填写的字段由宿主按语义补默认值。
export interface DialogOptions {
  /** title 覆盖默认标题；为空时按语义变体取默认标题。 */
  title?: string;
  /** variant 覆盖语义配色；为空时 alert 取 info，confirm 取 question。 */
  variant?: DialogVariant;
  /** confirmText 覆盖主按钮文案；为空时 alert 取“知道了”，其余取“确定”。 */
  confirmText?: string;
  /** cancelText 覆盖取消按钮文案；为空时取“取消”。 */
  cancelText?: string;
  /** placeholder 是输入型对话框的输入框占位文案。 */
  placeholder?: string;
}

// DialogRequest 是一次等待用户响应的对话框请求，由宿主按到达顺序逐条展示。
export interface DialogRequest {
  /** id 是请求的单调递增标识，用于把用户响应绑定到正确请求。 */
  id: number;
  /** kind 决定按钮数量与返回值的语义。 */
  kind: DialogKind;
  /** message 是展示给用户的正文，此处禁止放账号凭证等敏感信息。 */
  message: string;
  /** options 是调用方提供的展示定制项。 */
  options: DialogOptions;
  /** defaultValue 是输入型对话框的初始文本，其余类型为空串。 */
  defaultValue: string;
  /** resolve 把用户在对话框中的选择交回调用方，等待中的 Promise 因此收束。 */
  resolve: (value: string | boolean | null) => void;
}

// dialogQueue 保存尚未响应的对话框请求，先入先出保证连续提示按调用顺序出现。
let dialogQueue: readonly DialogRequest[] = [];

// dialogSubscribers 保存宿主渲染器的订阅回调，队列变化时逐个通知。
const dialogSubscribers = new Set<() => void>();

// dialogHostReady 标记居中模态宿主是否已挂载；为假时全部请求降级到浏览器原生弹框。
let dialogHostReady = false;

// dialogSequence 为每个新请求分配自增标识，跨请求保持唯一。
let dialogSequence = 0;

// notifyDialogSubscribers 通知已挂载的宿主重新读取队列快照。
const notifyDialogSubscribers = (): void => {
  dialogSubscribers.forEach(/* subscriber 是等待队列变化的宿主回调。 */ subscriber => subscriber());
};

// setDialogQueue 以新快照替换队列并唤醒宿主，整体替换可保持外部订阅的比较结果稳定。
const setDialogQueue = (nextQueue: readonly DialogRequest[]): void => {
  dialogQueue = nextQueue;
  notifyDialogSubscribers();
};

// subscribeDialog 注册宿主渲染器的队列订阅，返回取消订阅函数供 effect 清理。
export const subscribeDialog = (subscriber: () => void): (() => void) => {
  dialogSubscribers.add(subscriber);
  return /* unsubscribe 在宿主卸载时移除对应的队列订阅。 */ () => {
    dialogSubscribers.delete(subscriber);
  };
};

// getDialogSnapshot 返回当前对话框队列快照，队列未变化时必须返回同一引用。
export const getDialogSnapshot = (): readonly DialogRequest[] => dialogQueue;

// markDialogHostReady 由宿主在挂载时调用，此后所有提示统一走居中模态框。
export const markDialogHostReady = (): void => {
  dialogHostReady = true;
};

// markDialogHostGone 由宿主在卸载时调用并把未响应的请求按取消语义收束，避免调用方永久等待。
export const markDialogHostGone = (): void => {
  dialogHostReady = false;
  // pending 是宿主卸载前仍未得到用户响应的请求。
  const pending = dialogQueue;
  dialogQueue = [];
  notifyDialogSubscribers();
  pending.forEach(/* pendingRequest 逐条按取消语义收束，调用方不会悬挂。 */ pendingRequest => {
    pendingRequest.resolve(pendingRequest.kind === 'confirm' ? false : null);
  });
};

// enqueueDialog 把请求追加到队尾并通知宿主，调用方随后通过返回的 Promise 等待响应。
const enqueueDialog = (request: Omit<DialogRequest, 'id'>): void => {
  dialogSequence += 1;
  setDialogQueue([...dialogQueue, { ...request, id: dialogSequence }]);
};

// resolveDialog 由宿主在用户点击或按键后调用，只接受队首请求的响应以避免过期提交。
export const resolveDialog = (id: number, value: string | boolean | null): void => {
  // head 是当前正在展示的队首请求。
  const head = dialogQueue[0];
  if (!head || head.id !== id) return;
  setDialogQueue(dialogQueue.slice(1));
  head.resolve(value);
};

// dialogAlert 展示语义提示；宿主未挂载时降级为浏览器原生 alert。
const dialogAlert = (message: string, options: DialogOptions = {}): Promise<void> => {
  if (!dialogHostReady) {
    window.alert(message);
    return Promise.resolve();
  }
  return new Promise<void>(/* alertExecutor 把请求交给宿主并等待用户关闭。 */ resolve => {
    enqueueDialog({ kind: 'alert', message, options, defaultValue: '', resolve: /* alertResolve 提示类请求不携带选择结果。 */ () => resolve() });
  });
};

// dialogConfirm 请求用户确认；宿主未挂载时降级为浏览器原生 confirm。
const dialogConfirm = (message: string, options: DialogOptions = {}): Promise<boolean> => {
  if (!dialogHostReady) return Promise.resolve(window.confirm(message));
  return new Promise<boolean>(/* confirmExecutor 把确认请求交给宿主并等待用户选择。 */ resolve => {
    enqueueDialog({ kind: 'confirm', message, options, defaultValue: '', resolve: /* confirmResolve 只有显式确认才回传真值。 */ value => resolve(value === true) });
  });
};

// dialogPrompt 请求用户输入；宿主未挂载时降级为浏览器原生 prompt。
const dialogPrompt = (message: string, defaultValue = '', options: DialogOptions = {}): Promise<string | null> => {
  if (!dialogHostReady) return Promise.resolve(window.prompt(message, defaultValue));
  return new Promise<string | null>(/* promptExecutor 把输入请求交给宿主并等待用户提交或取消。 */ resolve => {
    enqueueDialog({ kind: 'prompt', message, options, defaultValue, resolve: /* promptResolve 取消时回传空值，提交时回传文本。 */ value => resolve(typeof value === 'string' ? value : null) });
  });
};

// appDialog 是业务代码使用的统一入口，替代 alert、confirm 与 prompt 三个原生函数。
export const appDialog = {
  alert: dialogAlert,
  confirm: dialogConfirm,
  prompt: dialogPrompt,
};
