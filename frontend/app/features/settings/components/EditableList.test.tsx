// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import { EditableList } from './EditableList';
import type { EditableListProps } from './EditableList';

// TestRow 是 EditableList 测试用的简单行数据类型。
interface TestRow {
  /** title 是行标题字段。 */
  title: string;
  /** 未知扩展键用于验证补丁合并时保留。 */
  [key: string]: unknown;
}

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

// renderList 用给定行数据渲染 EditableList 并返回 onChange 替身与渲染结果。
function renderList(overrides: Partial<EditableListProps<TestRow>> = {}) {
  // onChange 记录列表回写调用。
  const onChange = vi.fn();
  // props 是组装后的容器属性：行渲染为标题输入框。
  const props: EditableListProps<TestRow> = {
    rows: [{ title: '第一行' }, { title: '第二行' }],
    onChange,
    createRow: /* createRow 构造空标题新行。 */ () => ({ title: '' }),
    getRowKey: /* getRowKey 用行标题加索引作为 React key。 */ (row, index) => `${row.title}-${index}`,
    renderRow: (row, index, update) => (
      <input
        value={row.title}
        onChange={/* 输入变化合并当前行标题补丁。 */ event => update({ title: event.target.value })}
        aria-label={`第 ${index + 1} 行标题`}
      />
    ),
    addButtonText: '添加一行',
    emptyText: '列表为空',
    ...overrides,
  };
  // view 是当前容器渲染结果，供用例卸载后重绘下一场景。
  const view = render(<EditableList {...props} />);
  return { onChange, view };
}

describe('EditableList', /* 当前测试组验证通用可增删行容器的交互与行内校验。 */ () => {
  test('渲染已有行并支持编辑、新增与删除', /* 当前测试验证增删改三条列表主路径。 */ () => {
    // onChange 记录列表回写调用。
    const onChange = vi.fn();
    renderList({ onChange, rows: [{ title: '甲' }] });
    expect(screen.getByLabelText('第 1 行标题')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('第 1 行标题'), { target: { value: '甲改' } });
    expect(onChange).toHaveBeenLastCalledWith([{ title: '甲改' }]);
    fireEvent.click(screen.getByRole('button', { name: '添加一行' }));
    expect(onChange).toHaveBeenLastCalledWith([{ title: '甲' }, { title: '' }]);
    fireEvent.click(screen.getByRole('button', { name: '删除第 1 行' }));
    expect(onChange).toHaveBeenLastCalledWith([]);
  });

  test('空列表展示提示文案且校验错误就地红字展示', /* 当前测试验证空态与行内校验错误渲染。 */ () => {
    // emptyCase 是空列表场景的渲染结果，断言后卸载以重绘下一场景。
    const emptyCase = renderList({ rows: [] });
    expect(screen.getByText('列表为空')).toBeTruthy();
    emptyCase.view.unmount();
    renderList({
      rows: [{ title: '' }],
      validateRow: /* validateRow 对空标题行返回必填错误。 */ row => row.title.trim() === '' ? ['标题必填'] : [],
    });
    expect(screen.queryByText('列表为空')).toBeNull();
    expect(screen.getByText('标题必填')).toBeTruthy();
  });

  test('行字段补丁合并保留未知扩展键', /* 当前测试验证 update 补丁不丢行内未知键。 */ () => {
    // onChange 记录列表回写调用。
    const onChange = vi.fn();
    renderList({ onChange, rows: [{ title: '保留', extra: 'keep-me' }] });
    fireEvent.change(screen.getByLabelText('第 1 行标题'), { target: { value: '已改' } });
    expect(onChange).toHaveBeenLastCalledWith([{ title: '已改', extra: 'keep-me' }]);
  });
});
