// EditableList 是可增删行的通用列表容器：行内容由调用方渲染，
// 容器负责新增、删除与行内校验错误展示，供 FAQ / 在售清单 / 意图 / MCP 四处复用。

import React from 'react';
import { Plus, Trash2 } from 'lucide-react';

// EditableListProps 描述通用可增删行容器的输入契约。
export interface EditableListProps<T extends object> {
  /** rows 是当前列表数据；增删改后由容器整体回写。 */
  rows: T[];
  /** onChange 在列表增删或行字段变更时回写完整新数组。 */
  onChange: (rows: T[]) => void;
  /** createRow 构造新增一行的初始数据。 */
  createRow: () => T;
  /** getRowKey 返回单行 React key，常用稳定 ID 或索引拼接。 */
  getRowKey: (row: T, index: number) => string;
  /** renderRow 渲染单行编辑控件；update 合并该行字段补丁并回写列表。 */
  renderRow: (row: T, index: number, update: (patch: Partial<T>) => void) => React.ReactNode;
  /** validateRow 返回该行校验错误文案列表；空数组表示通过。 */
  validateRow?: (row: T, index: number) => string[];
  /** addButtonText 是底部新增按钮的文案。 */
  addButtonText: string;
  /** emptyText 是列表为空时的提示文案。 */
  emptyText?: string;
  /** rowNamePrefix 是删除按钮无障碍名称里的行类型前缀，用于区分同页多个列表。 */
  rowNamePrefix?: string;
}

// EditableList 渲染可增删行容器：每行一张边框卡片，错误就地红框红字展示。
export function EditableList<T extends object>(props: EditableListProps<T>): React.ReactElement {
  // rows 是当前列表数据。
  const { rows, onChange, createRow, getRowKey, renderRow, validateRow, addButtonText, emptyText, rowNamePrefix } = props;

  // deleteLabel 返回删除按钮的无障碍名称；带行类型前缀时用空格分隔，避免多列表撞名。
  const deleteLabel = (index: number) => rowNamePrefix ? `删除 ${rowNamePrefix} 第 ${index + 1} 行` : `删除第 ${index + 1} 行`;

  // updateRow 把指定行的字段补丁合并进该行并回写完整列表。
  const updateRow = (index: number, patch: Partial<T>) => {
    // nextRows 是合并当前行补丁后的新列表。
    const nextRows = rows.map(/* rowMerger 合并当前编辑行的字段补丁。 */ (row, rowIndex) => rowIndex === index ? { ...row, ...patch } : row);
    onChange(nextRows);
  };

  // addRow 在列表末尾追加一行初始数据。
  const addRow = () => {
    onChange([...rows, createRow()]);
  };

  // removeRow 删除指定索引行并回写剩余列表。
  const removeRow = (index: number) => {
    onChange(rows.filter(/* rowKeeper 保留未被删除的列表行。 */ (_row, rowIndex) => rowIndex !== index));
  };

  return (
    <div className="space-y-3">
      {rows.length === 0 && emptyText && (
        // emptyHint 是空列表引导文案。
        <p className="rounded-xl border border-dashed border-gray-200 bg-gray-50 px-4 py-3 text-xs text-gray-500">{emptyText}</p>
      )}
      <div className="space-y-3">
        {rows.map(/* rowRenderer 渲染当前列表行及行内校验错误。 */ (row, index) => {
          // errors 是当前行的校验错误文案列表。
          const errors = validateRow ? validateRow(row, index) : [];
          // hasError 表示当前行是否存在校验错误，决定红框样式。
          const hasError = errors.length > 0;
          return (
            <div
              key={getRowKey(row, index)}
              className={`rounded-xl border p-3 space-y-2 ${hasError ? 'border-red-300 bg-red-50/50' : 'border-gray-200 bg-white'}`}
            >
              <div className="flex items-start gap-2">
                <div className="flex-1 min-w-0">{renderRow(row, index, /* rowUpdater 合并当前行字段补丁。 */ patch => updateRow(index, patch))}</div>
                <button
                  type="button"
                  onClick={/* 当前回调删除当前列表行。 */ () => removeRow(index)}
                  className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-gray-400 transition-colors hover:bg-red-50 hover:text-red-500"
                  title={deleteLabel(index)}
                  aria-label={deleteLabel(index)}
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
              {errors.map(/* errorRenderer 展示当前行的单条校验错误。 */ (error, errorIndex) => (
                <p key={`${getRowKey(row, index)}-err-${errorIndex}`} className="text-xs text-red-500">{error}</p>
              ))}
            </div>
          );
        })}
      </div>
      <button
        type="button"
        onClick={addRow}
        className="inline-flex items-center gap-1.5 rounded-xl border border-dashed border-gray-300 px-3 py-2 text-xs font-bold text-gray-600 transition-colors hover:border-brand hover:text-brand"
      >
        <Plus className="h-3.5 w-3.5" />
        {addButtonText}
      </button>
    </div>
  );
}

export default EditableList;
