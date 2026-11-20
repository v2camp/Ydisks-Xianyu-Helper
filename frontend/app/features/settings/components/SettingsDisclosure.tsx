import { ChevronDown } from 'lucide-react';
import React, { useState } from 'react';
import { SECTION_TONE_STYLES } from '../../../../shared/ui/tokens';
import type { SectionTone } from '../../../../shared/ui/tokens';

// SettingsDisclosureProps 描述可折叠设置区块的标题、语义色与内容。
export interface SettingsDisclosureProps {
  /** icon 是区块标题图标，与 tone 一起构成视觉标识。 */
  icon: React.ElementType;
  /** title 是收起状态下唯一可见的标题文案。 */
  title: string;
  /** description 是标题下方的辅助说明，收起时截断为一行。 */
  description?: string;
  /** tone 是语义色，设置类区块缺省用中性灰弱化。 */
  tone?: SectionTone;
  /** children 是展开后显示的设置内容。 */
  children: React.ReactNode;
}

// SettingsDisclosure 渲染默认收起的设置区块：标题行即展开开关，把首屏空间让给会话主体。
export const SettingsDisclosure: React.FC<SettingsDisclosureProps> = ({
  icon: Icon, title, description, tone = 'slate', children,
}) => {
  // open 表示设置内容是否展开；默认收起，避免设置项占据第一屏。
  const [open, setOpen] = useState(false);

  // toggleOpen 切换设置内容的展开状态。
  const toggleOpen = () => setOpen(/* previous 是切换前的展开状态，返回取反值。 */ previous => !previous);

  return (
    <div className="ios-card overflow-hidden rounded-xl bg-white">
      <button
        type="button"
        aria-expanded={open}
        onClick={toggleOpen}
        className="flex w-full items-center justify-between gap-3 px-5 py-3.5 text-left transition-colors outline-none hover:bg-gray-50 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-brand-300"
      >
        <span className="flex min-w-0 items-center gap-2">
          <span className={`rounded-lg p-1.5 text-white ${SECTION_TONE_STYLES[tone]}`}>
            <Icon className="h-4 w-4" aria-hidden="true" />
          </span>
          <span className="min-w-0">
            <span className="block text-sm font-bold text-gray-800">{title}</span>
            {description && <span className="mt-0.5 block truncate text-xs text-gray-500">{description}</span>}
          </span>
        </span>
        <ChevronDown className={`h-4 w-4 shrink-0 text-gray-400 transition-transform ${open ? 'rotate-180' : ''}`} aria-hidden="true" />
      </button>
      {open && <div className="border-t border-gray-100 px-5 py-5">{children}</div>}
    </div>
  );
};

export default SettingsDisclosure;
