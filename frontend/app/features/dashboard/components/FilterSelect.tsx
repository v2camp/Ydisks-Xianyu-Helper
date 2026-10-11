import { ChevronDown } from 'lucide-react';
import React, { useEffect, useRef } from 'react';

/** FilterSelectProps 是仪表盘通用下拉筛选器的入参。 */
export interface FilterSelectProps {
  /** icon 是触发器左侧展示的筛选维度图标。 */
  icon: React.ElementType;
  /** label 是筛选维度名称，常驻展示在触发器上。 */
  label: string;
  /** value 是触发器上展示的当前选中值文本。 */
  value: string;
  /** ariaLabel 是下拉触发器的无障碍读屏名称。 */
  ariaLabel: string;
  /** open 表示面板是否展开；由父级持有，便于面板内按钮收起面板。 */
  open: boolean;
  /** onOpenChange 同步展开状态，用户点击触发器或请求关闭时触发。 */
  onOpenChange: (open: boolean) => void;
  /** panelClassName 控制下拉面板的宽度与对齐方式。 */
  panelClassName?: string;
  /** children 是下拉面板内容，展开时才渲染。 */
  children: React.ReactNode;
}

/** FilterSelect 渲染常驻一行的下拉筛选器：触发器固定占位，面板以浮层展开，不挤压同行动作区。 */
export const FilterSelect: React.FC<FilterSelectProps> = ({
  icon: Icon,
  label,
  value,
  ariaLabel,
  open,
  onOpenChange,
  panelClassName = 'w-64',
  children,
}) => {
  // containerRef 保存触发器与面板的公共容器，用于判定点击是否落在浮层内部。
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(/* 当前回调在面板展开期间接管全局点击与键盘事件，收起时释放监听。 */ () => {
    if (!open) return;
    // handlePointerDown 在点击落在容器之外时收起面板，避免浮层长期遮挡页面内容。
    const handlePointerDown = (/* event 是本次鼠标按下事件。 */ event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) onOpenChange(false);
    };
    // handleKeyDown 让 Esc 收起面板，键盘用户无需移动焦点即可退出浮层。
    const handleKeyDown = (/* event 是本次键盘事件。 */ event: KeyboardEvent) => {
      if (event.key === 'Escape') onOpenChange(false);
    };
    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return /* 当前回调移除监听，防止多个筛选器之间互相干扰。 */ () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [onOpenChange, open]);

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        aria-label={ariaLabel}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={/* 当前回调在用户点击触发器时切换面板展开状态。 */ () => onOpenChange(!open)}
        className={`flex items-center gap-2 rounded-xl border px-3.5 py-2.5 text-sm font-bold transition-colors ${
          open ? 'border-brand bg-brand/5 text-brand' : 'border-gray-200 bg-white text-gray-700 hover:border-gray-300'
        }`}
      >
        <Icon className="w-4 h-4" />
        <span className="font-medium text-gray-400">{label}</span>
        <span className="max-w-[10rem] truncate">{value}</span>
        <ChevronDown className={`w-4 h-4 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div role="menu" className={`absolute left-0 z-30 mt-2 rounded-2xl border border-gray-100 bg-white p-2 shadow-xl ${panelClassName}`}>
          {children}
        </div>
      )}
    </div>
  );
};
