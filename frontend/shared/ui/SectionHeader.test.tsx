// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { Box } from 'lucide-react';
import { afterEach, describe, expect, test } from 'vitest';
import { SectionHeader } from './SectionHeader';

afterEach(/* 每个用例结束后卸载渲染树，避免跨用例的 DOM 残留。 */ () => cleanup());

// TONE_CASES 是设计文档 §4.3 语义色板与图标槽底色的对应关系，供逐个色调校验。
const TONE_CASES = [
  { tone: 'brand', className: 'bg-brand' },
  { tone: 'violet', className: 'bg-violet-500' },
  { tone: 'emerald', className: 'bg-emerald-500' },
  { tone: 'indigo', className: 'bg-indigo-500' },
  { tone: 'slate', className: 'bg-slate-700' },
  { tone: 'amber', className: 'bg-amber-500' },
] as const;

describe('SectionHeader', /* 用例组验证区块标题的文案、动作槽与语义色映射。 */ () => {
  test('渲染标题、说明与右侧动作槽', /* 验证区块身份信息与局部操作同处一行。 */ () => {
    render(
      <SectionHeader
        icon={Box}
        title="外部 MCP 工具"
        description="由管理员维护的工具接入"
        actions={<button type="button">新增工具</button>}
      />,
    );
    expect(screen.getByRole('heading', { name: '外部 MCP 工具' })).toBeTruthy();
    expect(screen.getByText('由管理员维护的工具接入')).toBeTruthy();
    expect(screen.getByRole('button', { name: '新增工具' })).toBeTruthy();
  });

  test('缺省 tone 使用 brand 底色', /* 验证未显式传色时回落到 AI 能力域的主色。 */ () => {
    render(<SectionHeader icon={Box} title="模型与凭据" />);
    // iconSlot 是标题左侧缺省色调的图标槽。
    const iconSlot = screen.getByRole('heading', { name: '模型与凭据' }).previousElementSibling as HTMLElement;
    expect(iconSlot.className).toContain('bg-brand');
  });

  test('各语义色映射到约定的图标底色', /* 验证页面不再随手取色，色调由原语统一解释。 */ () => {
    TONE_CASES.forEach(/* toneCase 是当前待校验的色调与期望底色类名。 */ toneCase => {
      // view 是当前色调的渲染结果，断言后立即卸载以免标题重名。
      const view = render(<SectionHeader icon={Box} title={`区块-${toneCase.tone}`} tone={toneCase.tone} />);
      // iconSlot 是当前色调下标题左侧的图标槽。
      const iconSlot = screen.getByRole('heading', { name: `区块-${toneCase.tone}` }).previousElementSibling as HTMLElement;
      expect(iconSlot.className).toContain(toneCase.className);
      view.unmount();
    });
  });
});
