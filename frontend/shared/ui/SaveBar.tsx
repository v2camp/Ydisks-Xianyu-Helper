import { Check, Loader2, Save } from 'lucide-react';
import React, { useEffect } from 'react';

// SaveBarState 是保存条的四态：空闲待提交、保存中、保存失败、保存成功。
export type SaveBarState = 'idle' | 'saving' | 'error' | 'success';

// SaveBarProps 描述保存条的受控状态、提交回调与两种作用域下的展示文案。
export interface SaveBarProps {
  /** state 是保存条当前状态，由页面根据保存请求的进展驱动。 */
  state: SaveBarState;
  /** onSave 是主按钮与失败态「重试」按钮共同的提交回调。 */
  onSave: () => void;
  /** label 是主按钮文案，用于说明本次保存的对象，如「保存 AI 配置」。 */
  label: string;
  /** scopeLabel 是空闲态左侧的作用范围说明，缺省提示存在未保存修改。 */
  scopeLabel?: string;
  /** errorMessage 是失败态的可见错误文案，缺省为通用失败提示。 */
  errorMessage?: string;
  /** successMessage 是成功态的提示文案，缺省为「已保存」。 */
  successMessage?: string;
  /** sticky 表示页级粘性定位；区块级保存条传 false，留在文档流内避免遮挡下方内容。 */
  sticky?: boolean;
  /** onSettled 是成功提示展示满 2 秒后的回调，供页面把 state 复位为 idle。 */
  onSettled?: () => void;
  /** className 是调用方追加的类名，用于个别页面的间距微调。 */
  className?: string;
}

// SUCCESS_SETTLE_MS 是成功提示的停留时长：留出可读时间后自动回落，避免绿色条长期占位。
const SUCCESS_SETTLE_MS = 2000;

// SaveBar 渲染草稿型配置的保存条，同时支持页级粘性与区块级内嵌两种作用域。
export const SaveBar: React.FC<SaveBarProps> = ({
  state,
  onSave,
  label,
  scopeLabel = '有未保存的修改',
  errorMessage = '保存失败，请重试',
  successMessage = '已保存',
  sticky = false,
  onSettled,
  className,
}) => {
  useEffect(/* effect 在成功态挂载后启动 2 秒定时器，到期把控制权交还页面以复位保存态。 */ () => {
    if (state !== 'success' || !onSettled) return undefined;
    // settleTimer 是成功提示的回落定时器，状态切换或卸载时必须清理以免重复回调。
    const settleTimer = window.setTimeout(/* 定时器到期后通知页面把保存态复位为空闲。 */ () => onSettled(), SUCCESS_SETTLE_MS);
    return /* 清理未触发的回落定时器，避免成功条消失后仍触发一次状态复位。 */ () => window.clearTimeout(settleTimer);
  }, [state, onSettled]);

  // barToneClass 是保存条底色：成功与失败用语义色强调结果，空闲与保存中保持深色底承托主按钮。
  const barToneClass = state === 'error'
    ? 'bg-danger-500'
    : state === 'success'
      ? 'bg-success-500'
      : 'bg-slate-800';
  // stickyClass 是页级粘性定位类名；区块级保存条不加粘性，跟随区块内容滚动。
  const stickyClass = sticky ? 'sticky bottom-4 z-20' : '';

  return (
    <div
      className={`flex items-center justify-between gap-3 rounded-xl px-4 py-3 ${barToneClass} ${stickyClass} ${className ?? ''}`}
      role={state === 'error' ? 'alert' : 'status'}
    >
      {state === 'idle' && <span className="text-xs font-medium text-slate-300">{scopeLabel}</span>}
      {state === 'saving' && <span className="text-xs font-medium text-slate-300">{scopeLabel}</span>}
      {state === 'error' && <span className="text-sm font-bold text-white">{errorMessage}</span>}
      {state === 'success' && (
        <span className="flex items-center gap-2 text-sm font-bold text-white">
          <Check className="w-4 h-4" />
          {successMessage}
        </span>
      )}

      {state === 'idle' && (
        <button
          type="button"
          onClick={onSave}
          className="ios-btn-primary px-5 py-2.5 rounded-xl font-bold text-sm flex items-center gap-2 shrink-0"
        >
          <Save className="w-4 h-4" />
          {label}
        </button>
      )}
      {state === 'saving' && (
        <button
          type="button"
          disabled
          className="ios-btn-primary px-5 py-2.5 rounded-xl font-bold text-sm flex items-center gap-2 shrink-0"
        >
          <Loader2 className="w-4 h-4 animate-spin" />
          保存中...
        </button>
      )}
      {state === 'error' && (
        <button
          type="button"
          onClick={onSave}
          className="px-4 py-2 rounded-xl bg-white text-danger-600 hover:bg-danger-50 font-bold text-sm shrink-0 transition-colors"
        >
          重试
        </button>
      )}
    </div>
  );
};

export default SaveBar;
