import React from 'react';

// SectionTone 是区块标题的语义色板：颜色对应业务含义，避免各页面随手取色。
export type SectionTone = 'brand' | 'violet' | 'emerald' | 'indigo' | 'slate' | 'amber';

// SectionHeaderProps 描述区块标题的图标、文案与右侧动作槽。
export interface SectionHeaderProps {
  /** icon 是区块图标组件，与 tone 一起构成区块的视觉标识。 */
  icon: React.ElementType;
  /** title 是区块标题文案。 */
  title: string;
  /** description 是标题下方的一句话解释，缺省时只展示标题行。 */
  description?: string;
  /** tone 是语义色，缺省为 brand（AI 能力域）。 */
  tone?: SectionTone;
  /** actions 是标题行右侧的动作槽，用于放置该区块的局部操作。 */
  actions?: React.ReactNode;
}

// SECTION_TONE_CLASSES 把语义色映射到图标槽底色，取值与设计文档 §4.3 的色板一一对应。
const SECTION_TONE_CLASSES: Record<SectionTone, string> = {
  brand: 'bg-brand',
  violet: 'bg-violet-500',
  emerald: 'bg-emerald-500',
  indigo: 'bg-indigo-500',
  slate: 'bg-slate-700',
  amber: 'bg-amber-500',
};

// SectionHeader 渲染区块标题：语义色图标块 + 标题 + 可选说明与右侧动作槽。
export const SectionHeader: React.FC<SectionHeaderProps> = ({
  icon: Icon, title, description, tone = 'brand', actions,
}) => (
  <div className="space-y-1">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className={`p-1.5 rounded-lg text-white ${SECTION_TONE_CLASSES[tone]}`}>
          <Icon className="w-4 h-4" />
        </div>
        <h3 className="text-lg font-extrabold text-ink">{title}</h3>
      </div>
      {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
    </div>
    {description && <p className="text-xs leading-5 text-muted">{description}</p>}
  </div>
);

export default SectionHeader;
