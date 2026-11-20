// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { ShieldCheck } from 'lucide-react';
import { afterEach,describe,expect,test } from 'vitest';
import { SettingsDisclosure } from './SettingsDisclosure';

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

describe('SettingsDisclosure', /* 当前测试组验证设置区块默认收起与标题行展开收起。 */ () => {
  test('默认收起，点击标题行展开并可再次收起', /* 当前测试验证展开状态与内容挂载同步。 */ () => {
    render((
      <SettingsDisclosure icon={ShieldCheck} title="测试设置" description="附属说明">
        <p>设置内容</p>
      </SettingsDisclosure>
    ));
    // toggle 是标题行展开开关，accessible name 含标题与说明。
    const toggle = screen.getByRole('button', { name: /测试设置/ });
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('设置内容')).toBeNull();
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('设置内容')).toBeTruthy();
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('设置内容')).toBeNull();
  });
});
