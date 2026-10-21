import React from 'react';
import { StatusBadge, type StatusBadgeTone } from './StatusBadge';

// DependencyState 是只读依赖的四态：读取中、已配置、未配置但回落内置、彻底未配置。
export type DependencyState = 'loading' | 'ready' | 'fallback' | 'missing';

// DependencyRowProps 描述依赖行的名称、状态、补充说明与跳转入口。
export interface DependencyRowProps {
  /** label 是依赖的名称，例如「QQ 机器人凭据」。 */
  label: string;
  /** state 是依赖当前状态，决定徽章文案与语义色。 */
  state: DependencyState;
  /** detail 是依赖的补充说明，例如「未配置时使用内置模型」，缺省不占位。 */
  detail?: string;
  /** linkLabel 是跳转按钮文案，例如「去连接器页配置」；缺省时不渲染跳转入口。 */
  linkLabel?: string;
  /** onNavigate 是跳转按钮的点击回调，与 linkLabel 同时存在时才渲染按钮。 */
  onNavigate?: () => void;
  /** testId 是稳定的测试标识，供页面级用例定位该依赖行。 */
  testId?: string;
}

// DependencyView 描述单一依赖态在界面上的呈现方式。
interface DependencyView {
  /** badgeLabel 是徽章文案，直接面向用户说明该依赖的可用状态。 */
  badgeLabel: string;
  /** tone 是徽章语义色，与 StatusBadge 的色板保持一致。 */
  tone: StatusBadgeTone;
}

// DEPENDENCY_VIEWS 把依赖四态映射为徽章文案与语义色：读取中不误报未配置，回落内置用琥珀弱化区分于彻底未配置。
const DEPENDENCY_VIEWS: Record<DependencyState, DependencyView> = {
  loading: { badgeLabel: '读取中…', tone: 'loading' },
  ready: { badgeLabel: '已配置', tone: 'ready' },
  fallback: { badgeLabel: '未配置 · 回落内置', tone: 'warning' },
  missing: { badgeLabel: '未配置', tone: 'missing' },
};

// DependencyRow 渲染只读依赖行：依赖名 + 状态徽章 + 跳转目标，整体弱化以免与主配置区争夺视觉权重。
export const DependencyRow: React.FC<DependencyRowProps> = ({
  label, state, detail, linkLabel, onNavigate, testId,
}) => {
  // view 是当前状态对应的徽章文案与语义色。
  const view = DEPENDENCY_VIEWS[state];

  return (
    <div
      data-testid={testId}
      className="flex items-center justify-between gap-3 rounded-xl border border-gray-200 bg-white px-4 py-3"
    >
      <div className="min-w-0">
        <div className="text-sm font-bold text-ink">{label}</div>
        {detail && <p className="mt-0.5 text-xs leading-5 text-muted">{detail}</p>}
      </div>
      <div className="flex items-center gap-3 shrink-0">
        <StatusBadge tone={view.tone}>{view.badgeLabel}</StatusBadge>
        {linkLabel && onNavigate && (
          <button
            type="button"
            onClick={onNavigate}
            className="text-xs font-bold text-brand hover:text-brand-highlight transition-colors"
          >
            {linkLabel} →
          </button>
        )}
      </div>
    </div>
  );
};

export default DependencyRow;
