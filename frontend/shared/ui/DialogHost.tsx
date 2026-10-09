import { AlertTriangle,CheckCircle2,HelpCircle,Info,XCircle } from 'lucide-react';
import React from 'react';
import {
  getDialogSnapshot,
  markDialogHostGone,
  markDialogHostReady,
  resolveDialog,
  subscribeDialog,
  type DialogRequest,
  type DialogVariant,
} from './dialog';

// DialogIconProps 描述语义图标组件接收的样式入口。
interface DialogIconProps {
  /** className 是图标组件接收的样式类名。 */
  className?: string;
}

// DialogVariantMeta 描述一种语义变体在弹窗中的图标、徽章配色与默认标题。
interface DialogVariantMeta {
  /** Icon 是语义变体对应的图标组件。 */
  Icon: React.ComponentType<DialogIconProps>;
  /** badgeClass 是图标徽章的背景与前景配色。 */
  badgeClass: string;
  /** defaultTitle 是调用方未指定标题时展示的文案。 */
  defaultTitle: string;
}

// DIALOG_VARIANTS 汇总全部语义变体的展示参数，组件不自行拼接颜色值。
const DIALOG_VARIANTS: Record<DialogVariant, DialogVariantMeta> = {
  info: { Icon: Info, badgeClass: 'bg-blue-50 text-blue-600', defaultTitle: '提示' },
  success: { Icon: CheckCircle2, badgeClass: 'bg-success-50 text-success-600', defaultTitle: '操作成功' },
  warning: { Icon: AlertTriangle, badgeClass: 'bg-warning-50 text-warning-600', defaultTitle: '请注意' },
  error: { Icon: XCircle, badgeClass: 'bg-danger-50 text-danger-600', defaultTitle: '操作失败' },
  question: { Icon: HelpCircle, badgeClass: 'bg-blue-50 text-blue-600', defaultTitle: '请确认' },
  danger: { Icon: AlertTriangle, badgeClass: 'bg-danger-50 text-danger-600', defaultTitle: '请确认' },
};

// FOCUSABLE_SELECTOR 约束对话框内的键盘焦点只在可交互控件之间环绕。
const FOCUSABLE_SELECTOR = 'button:not([disabled]), input:not([disabled])';

// resolveDialogVariant 按调用方配置与交互类型推导实际语义变体。
const resolveDialogVariant = (request: DialogRequest): DialogVariant => (
  request.options.variant ?? (request.kind === 'confirm' ? 'question' : 'info')
);

// DialogViewProps 描述单个对话框视图的渲染输入。
interface DialogViewProps {
  /** request 是当前正在展示的对话框请求。 */
  request: DialogRequest;
}

// DialogView 渲染单个居中对话框，负责焦点管理、回车确认、Escape 取消与遮罩点击取消。
const DialogView: React.FC<DialogViewProps> = ({ request }) => {
  // variant 是当前请求实际使用的语义变体。
  const variant = resolveDialogVariant(request);
  // meta 是当前语义变体的图标与配色参数。
  const meta = DIALOG_VARIANTS[variant];
  // VariantIcon 是该语义变体使用的图标组件。
  const VariantIcon = meta.Icon;
  // draft 保存输入型对话框的当前文本，非输入类型保持为空串。
  const [draft, setDraft] = React.useState(request.defaultValue);
  // panelRef 指向对话框主体，用于约束键盘焦点。
  const panelRef = React.useRef<HTMLDivElement | null>(null);
  // primaryRef 指向主操作按钮，作为 alert 与 confirm 的默认焦点。
  const primaryRef = React.useRef<HTMLButtonElement | null>(null);
  // inputRef 指向输入框，作为 prompt 的默认焦点。
  const inputRef = React.useRef<HTMLInputElement | null>(null);
  // previousFocusRef 保存弹窗打开前的焦点元素，关闭后恢复页面上下文。
  const previousFocusRef = React.useRef<HTMLElement | null>(null);
  // titleId 是该对话框标题的稳定无障碍标识。
  const titleId = React.useId();
  // descriptionId 是该对话框正文的稳定无障碍标识。
  const descriptionId = React.useId();

  // confirmDialog 按交互类型提交确认结果并收束当前请求。
  const confirmDialog = React.useCallback(/* confirmAction 提交确认：输入型回传文本，其余回传真值。 */ () => {
    resolveDialog(request.id, request.kind === 'prompt' ? draft : true);
  }, [draft, request.id, request.kind]);

  // cancelDialog 按交互类型提交取消结果并收束当前请求。
  const cancelDialog = React.useCallback(/* cancelAction 提交取消：确认型回传真假值中的假值，输入型回传空值。 */ () => {
    resolveDialog(request.id, request.kind === 'confirm' ? false : null);
  }, [request.id, request.kind]);

  React.useEffect(/* 当前副作用固定默认焦点，并在关闭后归还此前焦点。 */ () => {
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    // focusFrame 等待对话框挂载完成后把焦点放到输入框或主按钮。
    const focusFrame = window.requestAnimationFrame(/* focusCallback 选择该交互类型的默认焦点目标。 */ () => {
      if (request.kind === 'prompt') inputRef.current?.focus();
      else primaryRef.current?.focus();
    });
    return /* cleanup 取消延迟聚焦并把焦点交还打开前的元素。 */ () => {
      window.cancelAnimationFrame(focusFrame);
      previousFocusRef.current?.focus();
    };
  }, [request.id, request.kind]);

  // handleKeyDown 处理回车确认、Escape 取消与 Tab 焦点环绕。
  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>): void => {
    if (event.key === 'Escape') {
      event.preventDefault();
      cancelDialog();
      return;
    }
    if (event.key === 'Enter') {
      // 焦点落在按钮上时交给浏览器原生点击，避免同一次回车重复提交。
      if (event.target instanceof HTMLButtonElement) return;
      event.preventDefault();
      confirmDialog();
      return;
    }
    if (event.key !== 'Tab' || !panelRef.current) return;
    // focusable 保存对话框内当前可聚焦的控件。
    const focusable = [...panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)];
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }
    // first 是焦点环绕的起点控件。
    const [first] = focusable;
    // last 是焦点环绕的终点控件。
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };

  // confirmText 是主按钮展示文案，按交互类型给出默认值。
  const confirmText = request.options.confirmText ?? (request.kind === 'alert' ? '知道了' : '确定');
  // cancelText 是取消按钮展示文案。
  const cancelText = request.options.cancelText ?? '取消';
  // primaryClass 是主按钮配色，危险语义使用红色警示。
  const primaryClass = variant === 'danger'
    ? 'bg-danger-600 text-white hover:bg-danger-700 focus:ring-danger-100'
    : 'bg-brand text-white hover:bg-brand-highlight focus:ring-brand/20';

  return (
    <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-slate-950/50 p-4 backdrop-blur-[2px] animate-dialog-overlay" onMouseDown={/* 当前回调只在点击遮罩本身时按取消处理。 */ event => { if (event.target === event.currentTarget) cancelDialog(); }}>
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
        onKeyDown={handleKeyDown}
        className="w-full max-w-md animate-dialog-panel overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl"
      >
        <div className="flex items-start gap-4 px-6 pb-5 pt-6">
          <div className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl ${meta.badgeClass}`}>
            <VariantIcon className="h-5 w-5" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className="text-base font-black tracking-tight text-slate-950">{request.options.title ?? meta.defaultTitle}</h2>
            <p id={descriptionId} className="mt-2 whitespace-pre-wrap break-words text-sm font-medium leading-6 text-slate-600">{request.message}</p>
          </div>
        </div>

        {request.kind === 'prompt' && (
          <div className="px-6 pb-5">
            <input
              ref={inputRef}
              value={draft}
              onChange={/* 当前回调把输入框的实时文本写回草稿。 */ event => setDraft(event.target.value)}
              placeholder={request.options.placeholder}
              aria-label={request.options.title ?? meta.defaultTitle}
              className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm font-semibold text-slate-900 outline-none transition focus:border-brand focus:bg-white focus:ring-4 focus:ring-brand/10"
            />
          </div>
        )}

        <div className="flex justify-end gap-2 border-t border-slate-100 bg-slate-50/70 px-6 py-4">
          {request.kind !== 'alert' && (
            <button
              type="button"
              onClick={cancelDialog}
              className="rounded-xl border border-slate-200 bg-white px-4 py-2 text-sm font-bold text-slate-700 transition hover:bg-slate-50 focus:outline-none focus:ring-4 focus:ring-slate-200"
            >
              {cancelText}
            </button>
          )}
          <button
            ref={primaryRef}
            type="button"
            onClick={confirmDialog}
            className={`min-w-24 rounded-xl px-4 py-2 text-sm font-bold transition focus:outline-none focus:ring-4 ${primaryClass}`}
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>
  );
};

// DialogHost 在应用根挂载唯一的居中模态宿主，并按到达顺序逐条展示对话框请求。
export const DialogHost: React.FC = () => {
  // requests 是当前等待响应的对话框队列快照。
  const requests = React.useSyncExternalStore(subscribeDialog, getDialogSnapshot, getDialogSnapshot);
  React.useEffect(/* 当前副作用标记宿主就绪，并在卸载时收束未响应请求。 */ () => {
    markDialogHostReady();
    return /* cleanup 让宿主失效并释放等待中的调用方。 */ () => markDialogHostGone();
  }, []);
  // current 是队首请求，即当前唯一展示的对话框。
  const current = requests[0];
  if (!current) return null;
  return <DialogView key={current.id} request={current} />;
};

export default DialogHost;
