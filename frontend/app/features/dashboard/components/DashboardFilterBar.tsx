import { CalendarRange,UserRound } from 'lucide-react';
import React, { useMemo, useState } from 'react';
import { getDateRange, type TimeRange } from '../../../../dateRange';
import type { DashboardAccountOption } from '../models';
import { CustomRangePanel } from './CustomRangePanel';
import { FilterSelect } from './FilterSelect';

/** DashboardFilterBarProps 是运营仪表盘过滤器的入参。 */
export interface DashboardFilterBarProps {
  /** range 是当前生效的时间范围。 */
  range: TimeRange;
  /** customStartDate 是当前生效的自定义起始日期，格式为 YYYY-MM-DD。 */
  customStartDate: string;
  /** customEndDate 是当前生效的自定义结束日期，格式为 YYYY-MM-DD。 */
  customEndDate: string;
  /** cookieID 是当前生效的闲鱼账号标识，空串表示全部账号。 */
  cookieID: string;
  /** accounts 是账号下拉可选项，为空时只提供「全部账号」。 */
  accounts: DashboardAccountOption[];
  /** summary 是右侧展示的当前统计口径摘要。 */
  summary: string;
  /** onRangeChange 在用户选择快捷区间时立即切换生效范围。 */
  onRangeChange: (range: TimeRange) => void;
  /** onCustomRangeApply 在用户确认自定义区间时切换为自定义并重新拉取数据。 */
  onCustomRangeApply: (startDate: string, endDate: string) => void;
  /** onCookieChange 在用户切换账号时立即重新拉取全部下游数据。 */
  onCookieChange: (cookieID: string) => void;
}

// RANGE_PRESETS 是时间下拉中的快捷区间选项，自定义不在其中而单独进入区间编辑面板。
const RANGE_PRESETS: Array<{ /** key 是快捷区间标识。 */ key: Exclude<TimeRange, 'custom'>; /** label 是选项展示文本。 */ label: string }> = [
  { key: 'today', label: '今天' },
  { key: 'yesterday', label: '昨天' },
  { key: '3days', label: '3 天内' },
  { key: '7days', label: '7 天内' },
  { key: '30days', label: '1 个月内' },
];

// RANGE_LABELS 是时间范围到触发器展示文本的映射。
const RANGE_LABELS: Record<Exclude<TimeRange, 'custom'>, string> = {
  today: '今天',
  yesterday: '昨天',
  '3days': '3 天内',
  '7days': '7 天内',
  '30days': '1 个月内',
};

/** shortDate 把 YYYY-MM-DD 压缩为 MM-DD，空值返回占位符。 */
const shortDate = (/* value 是待压缩的日期文本。 */ value: string): string => value ? value.slice(5) : '--';

/** DashboardFilterBar 渲染时间范围与账号两个下拉筛选器；自定义区间在下拉内部纵向展开，不横向占用空间。 */
export const DashboardFilterBar: React.FC<DashboardFilterBarProps> = ({
  range,
  customStartDate,
  customEndDate,
  cookieID,
  accounts,
  summary,
  onRangeChange,
  onCustomRangeApply,
  onCookieChange,
}) => {
  // [rangeOpen, 时间下拉是否展开，属于瞬态 UI 状态，不参与数据请求。
  const [rangeOpen, setRangeOpen] = useState(false);
  // [accountOpen, 账号下拉是否展开，属于瞬态 UI 状态，不参与数据请求。
  const [accountOpen, setAccountOpen] = useState(false);
  // [customEditing, 时间下拉是否处于自定义区间编辑视图，而非快捷区间列表。
  const [customEditing, setCustomEditing] = useState(false);
  // [draftStart, 自定义起始日期草稿，只在用户点确定后才生效。
  const [draftStart, setDraftStart] = useState('');
  // [draftEnd, 自定义结束日期草稿，只在用户点确定后才生效。
  const [draftEnd, setDraftEnd] = useState('');

  // effectiveRange 是当前生效统计区间，用作自定义面板打开时的初始草稿。
  const effectiveRange = useMemo(
    // fallback 在日期非法时退化为原始自定义输入，保证面板仍可编辑。
    () => {
      try {
        return getDateRange(range, new Date(), customStartDate, customEndDate);
      } catch {
        return { startDate: customStartDate, endDate: customEndDate };
      }
    },
    [customEndDate, customStartDate, range],
  );
  // rangeValue 是时间下拉触发器上展示的当前区间文本。
  const rangeValue = range === 'custom' && customStartDate && customEndDate
    ? `${shortDate(customStartDate)} ~ ${shortDate(customEndDate)}`
    : range === 'custom' ? '自定义' : RANGE_LABELS[range];
  // accountValue 是账号下拉触发器上展示的当前账号名称。
  const accountValue = cookieID ? accounts.find(/* account 是当前待匹配的账号候选项。 */ account => account.id === cookieID)?.name || cookieID : '全部账号';

  // handleRangeOpenChange 在展开时用当前生效区间初始化草稿，在收起时回到快捷区间列表。
  const handleRangeOpenChange = (/* nextOpen 是目标展开状态。 */ nextOpen: boolean) => {
    setRangeOpen(nextOpen);
    if (!nextOpen) {
      setCustomEditing(false);
      return;
    }
    setCustomEditing(range === 'custom');
    setDraftStart(effectiveRange.startDate);
    setDraftEnd(effectiveRange.endDate);
  };
  // handlePresetSelect 在用户选择快捷区间时立即生效并收起面板。
  const handlePresetSelect = (/* nextRange 是用户选择的快捷区间。 */ nextRange: Exclude<TimeRange, 'custom'>) => {
    onRangeChange(nextRange);
    setCustomEditing(false);
    setRangeOpen(false);
  };
  // handleCustomApply 在用户确认自定义区间时提交草稿并收起面板。
  const handleCustomApply = () => {
    onCustomRangeApply(draftStart, draftEnd);
    setCustomEditing(false);
    setRangeOpen(false);
  };
  // handleAccountSelect 在用户切换账号时立即生效并收起面板。
  const handleAccountSelect = (/* nextCookieID 是用户选择的账号标识，空串表示全部账号。 */ nextCookieID: string) => {
    onCookieChange(nextCookieID);
    setAccountOpen(false);
  };

  return (
    <div className="ios-card flex flex-wrap items-center gap-3 px-4 py-3">
      <FilterSelect
        icon={CalendarRange}
        label="时间"
        value={rangeValue}
        ariaLabel="选择统计时间范围"
        open={rangeOpen}
        onOpenChange={handleRangeOpenChange}
        panelClassName="w-64"
      >
        {customEditing ? (
          <CustomRangePanel
            startDate={draftStart}
            endDate={draftEnd}
            onDraftChange={/* 当前回调同步用户正在编辑的起止日期草稿。 */ (startDate, endDate) => { setDraftStart(startDate); setDraftEnd(endDate); }}
            onApply={handleCustomApply}
            onCancel={/* 当前回调放弃自定义编辑并回到快捷区间列表。 */ () => setCustomEditing(false)}
          />
        ) : (
          <div className="p-1">
            {RANGE_PRESETS.map(/* preset 是当前待渲染的快捷区间选项。 */ preset => (
              <button
                key={preset.key}
                type="button"
                role="menuitem"
                onClick={/* 当前回调应用快捷区间并收起面板。 */ () => handlePresetSelect(preset.key)}
                className={`flex w-full items-center justify-between rounded-xl px-3 py-2 text-sm font-bold transition-colors ${
                  range === preset.key ? 'bg-brand/10 text-brand' : 'text-gray-600 hover:bg-gray-50'
                }`}
              >
                {preset.label}
                {range === preset.key && <span className="text-xs">当前</span>}
              </button>
            ))}
            <button
              type="button"
              role="menuitem"
              onClick={/* 当前回调在时间下拉内部切换到自定义区间编辑视图。 */ () => setCustomEditing(true)}
              className="mt-1 flex w-full items-center justify-between rounded-xl border-t border-gray-100 px-3 py-2 text-sm font-bold text-gray-600 hover:bg-gray-50"
            >
              自定义区间
              <span className="text-xs font-medium text-gray-400">选择起止日期</span>
            </button>
          </div>
        )}
      </FilterSelect>

      <FilterSelect
        icon={UserRound}
        label="账号"
        value={accountValue}
        ariaLabel="按闲鱼账号筛选"
        open={accountOpen}
        onOpenChange={setAccountOpen}
        panelClassName="w-64"
      >
        <div className="p-1">
          <button
            type="button"
            role="menuitem"
            onClick={/* 当前回调清除账号过滤，恢复为全部账号统计。 */ () => handleAccountSelect('')}
            className={`flex w-full items-center justify-between rounded-xl px-3 py-2 text-sm font-bold transition-colors ${
              cookieID === '' ? 'bg-brand/10 text-brand' : 'text-gray-600 hover:bg-gray-50'
            }`}
          >
            全部账号
            {cookieID === '' && <span className="text-xs">当前</span>}
          </button>
          {accounts.map(/* account 是当前待渲染的账号选项。 */ account => (
            <button
              key={account.id}
              type="button"
              role="menuitem"
              onClick={/* 当前回调按单个闲鱼账号过滤全部下游数据。 */ () => handleAccountSelect(account.id)}
              className={`flex w-full items-center justify-between gap-2 rounded-xl px-3 py-2 text-sm font-bold transition-colors ${
                cookieID === account.id ? 'bg-brand/10 text-brand' : 'text-gray-600 hover:bg-gray-50'
              }`}
            >
              <span className="truncate">{account.name}</span>
              {cookieID === account.id && <span className="text-xs">当前</span>}
            </button>
          ))}
        </div>
      </FilterSelect>

      <span className="ml-auto text-xs font-medium text-gray-400">{summary}</span>
    </div>
  );
};
