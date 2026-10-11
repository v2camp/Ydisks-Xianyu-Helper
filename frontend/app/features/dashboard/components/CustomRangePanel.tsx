import React from 'react';
import { formatLocalDate } from '../../../../dateRange';

/** CustomRangePanelProps 是自定义日期区间编辑面板的入参。 */
export interface CustomRangePanelProps {
  /** startDate 是起始日期草稿，格式为 YYYY-MM-DD。 */
  startDate: string;
  /** endDate 是结束日期草稿，格式为 YYYY-MM-DD。 */
  endDate: string;
  /** onDraftChange 在用户修改任一日期时同步草稿，未确认的输入不会触发请求。 */
  onDraftChange: (startDate: string, endDate: string) => void;
  /** onApply 在用户确认草稿区间时提交，调用方负责切换范围并重新拉取数据。 */
  onApply: () => void;
  /** onCancel 在用户放弃编辑时回到快捷区间列表。 */
  onCancel: () => void;
}

/** CustomRangePanel 在时间下拉内部纵向展开起止日期编辑区，避免自定义区间横向挤占过滤器整行。 */
export const CustomRangePanel: React.FC<CustomRangePanelProps> = ({ startDate, endDate, onDraftChange, onApply, onCancel }) => {
  // invalid 是草稿区间非法的原因文本，由日期直接派生，不额外存放状态。
  const invalid = startDate !== '' && endDate !== '' && startDate > endDate ? '开始日期不能晚于结束日期' : '';
  // canApply 表示起止日期均已填写且顺序正确，允许提交。
  const canApply = startDate !== '' && endDate !== '' && invalid === '';

  // applySpan 把起止日期草稿替换为以今天为终点的指定天数区间。
  const applySpan = (/* days 是从今天往前回溯的天数，含今天。 */ days: number) => {
    // end 是区间的结束日期，取当前本地日期。
    const end = new Date();
    // start 是区间起点，由结束日期回溯天数得到。
    const start = new Date();
    start.setDate(start.getDate() - (days - 1));
    onDraftChange(formatLocalDate(start), formatLocalDate(end));
  };

  return (
    <div className="p-1">
      <div className="grid grid-cols-1 gap-2">
        <label className="flex items-center justify-between gap-2 text-xs font-bold text-gray-500">
          开始
          <input
            type="date"
            aria-label="自定义开始日期"
            value={startDate}
            max={endDate || undefined}
            onChange={/* 当前回调在用户选择开始日期时更新草稿。 */ event => onDraftChange(event.target.value, endDate)}
            className="ios-input rounded-xl px-2 py-1.5 text-sm font-medium text-gray-900"
          />
        </label>
        <label className="flex items-center justify-between gap-2 text-xs font-bold text-gray-500">
          结束
          <input
            type="date"
            aria-label="自定义结束日期"
            value={endDate}
            min={startDate || undefined}
            onChange={/* 当前回调在用户选择结束日期时更新草稿。 */ event => onDraftChange(startDate, event.target.value)}
            className="ios-input rounded-xl px-2 py-1.5 text-sm font-medium text-gray-900"
          />
        </label>
      </div>

      <div className="mt-2 flex items-center gap-1.5">
        {[7, 30].map(/* days 是当前快捷跨度按钮对应的天数。 */ days => (
          <button
            key={days}
            type="button"
            onClick={/* 当前回调把草稿替换为最近若干天的区间。 */ () => applySpan(days)}
            className="rounded-lg bg-gray-100 px-2.5 py-1 text-xs font-bold text-gray-600 hover:bg-gray-200"
          >
            近 {days} 天
          </button>
        ))}
      </div>

      {invalid !== '' && <p className="mt-2 text-xs font-bold text-red-600">{invalid}</p>}

      <div className="mt-3 flex items-center justify-end gap-2">
        <button
          type="button"
          onClick={/* 当前回调放弃草稿并回到快捷区间列表。 */ onCancel}
          className="rounded-xl px-3 py-1.5 text-xs font-bold text-gray-500 hover:bg-gray-100"
        >
          取消
        </button>
        <button
          type="button"
          disabled={!canApply}
          onClick={/* 当前回调提交合法草稿并收起整个时间下拉。 */ onApply}
          className="ios-btn-primary rounded-xl px-3.5 py-1.5 text-xs"
        >
          确定
        </button>
      </div>
    </div>
  );
};
