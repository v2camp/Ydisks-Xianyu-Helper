// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { AccountAgentSupport,AccountDetail,AIReplySettings } from '../api';
import { AccountAISettingsModal,INHERIT_AGENT_VALUE } from './AccountAISettingsModal';
import { AccountCard } from './AccountCard';
import { AccountDeleteDialog } from './AccountDeleteDialog';
import { AccountQRCodeModal } from './AccountQRCodeModal';

// accountFixture 是账号组件测试使用的最小非敏感账号摘要。
const accountFixture = {
  id: 'account-1',
  nickname: '测试账号',
  remark: '测试备注',
  enabled: true,
  runtime_state: 'online',
  runtime_message: '',
  ai_enabled: true,
  auto_rate_enabled: false,
  auto_polish_enabled: false,
  auto_confirm: false,
  auto_consign: true,
  auto_bargain: false,
  paused: false,
} as AccountDetail;

// aiSettingsFixture 是 AI 设置弹窗测试使用的编辑草稿。
const aiSettingsFixture: AIReplySettings = {
  ai_enabled: false,
  auto_adjust_price_enabled: false,
  max_discount_percent: 10,
  max_discount_amount: 100,
  max_bargain_rounds: 3,
  custom_prompts: '',
};

// agentSupportFixture 是账号级客服 Agent 授权弹窗测试使用的草稿，两个覆盖值均为空表示继承租户默认。
const agentSupportFixture: AccountAgentSupport = {
  enabled: null,
  preset: null,
  effectiveEnabled: true,
  effectivePreset: 'readonly',
  presets: [
    { value: 'readonly', label: '只读', description: '只查询与答复，不产生对外动作' },
    { value: 'advanced', label: '高级', description: '在标准之上允许可逆的数据写入' },
  ],
};

// noopAccountAction 是账号卡片测试使用的动作占位函数。
const noopAccountAction = (): void => undefined;

describe('账号 feature 展示组件', /* 当前回调覆盖账号页面子模块展示边界。 */ () => {
  afterEach(/* 当前回调清理 Portal 和测试 DOM。 */ () => cleanup());

  test('账号卡片展示状态并转发所有操作', /* 当前回调验证账号卡片的操作边界。 */ () => {
    // onDelete 是删除操作测试替身。
    const onDelete = vi.fn();
    // onAI 是 AI 设置操作测试替身。
    const onAI = vi.fn();
    // onToggle 是启停操作测试替身。
    const onToggle = vi.fn();
    render(<AccountCard account={accountFixture} refreshing={false} deleting={false} onRefreshProfile={noopAccountAction} onReauthorize={noopAccountAction} onEdit={noopAccountAction} onAI={onAI} onTasks={noopAccountAction} onToggle={onToggle} onDelete={onDelete} />);
    expect(screen.getByText('测试账号')).toBeTruthy();
    fireEvent.click(screen.getByTitle('AI设置'));
    fireEvent.click(screen.getByTitle('停用账号'));
    fireEvent.click(screen.getByTitle('删除账号 测试账号'));
    expect(onAI).toHaveBeenCalledWith(accountFixture);
    expect(onToggle).toHaveBeenCalledWith(accountFixture.id, accountFixture.enabled);
    expect(onDelete).toHaveBeenCalledWith(accountFixture);
  });

  test('账号卡片常显自动确认发货与自动免拼状态', /* 当前回调验证发货相关开关在卡片上的外显边界。 */ () => {
    render(<AccountCard account={accountFixture} refreshing={false} deleting={false} onRefreshProfile={noopAccountAction} onReauthorize={noopAccountAction} onEdit={noopAccountAction} onAI={noopAccountAction} onTasks={noopAccountAction} onToggle={noopAccountAction} onDelete={noopAccountAction} />);
    // 自动确认发货在 fixture 中开启，自动免拼关闭；两者都必须常显以便一眼识别半截发货配置。
    expect(screen.getByText('自动确认发货')).toBeTruthy();
    expect(screen.getByText('自动免拼')).toBeTruthy();
    expect(screen.getByTitle('发完卡密后会自动点发货')).toBeTruthy();
    expect(screen.getByTitle('待刀成阶段不会自动免拼')).toBeTruthy();
  });

  test('AI 设置弹窗使用补丁更新并转发保存', /* 当前回调验证 AI 设置字段更新和保存动作。 */ () => {
    // onChange 是 AI 设置草稿更新测试替身。
    const onChange = vi.fn();
    // onSave 是 AI 设置保存测试替身。
    const onSave = vi.fn();
    // view 允许测试在 AI 开启后重新渲染同一受控弹窗。
    const view = render(<AccountAISettingsModal account={accountFixture} settings={aiSettingsFixture} agentSupport={agentSupportFixture} saving={false} onChange={onChange} onAgentChange={noopAccountAction} onClose={noopAccountAction} onSave={onSave} />);
    fireEvent.click(screen.getByLabelText('切换 AI 自动回复'));
    fireEvent.change(screen.getByDisplayValue('10'), { target: { value: '20' } });
    fireEvent.click(screen.getByText('保存'));
    expect(onChange).toHaveBeenNthCalledWith(1, { ...aiSettingsFixture, ai_enabled: true });
    expect(onChange).toHaveBeenNthCalledWith(2, { ...aiSettingsFixture, max_discount_percent: 20 });
    expect(onSave).toHaveBeenCalledTimes(1);
    // enabledSettings 是 AI 议价已开启、允许商家进一步选择真实自动改价的草稿。
    const enabledSettings = { ...aiSettingsFixture, ai_enabled: true };
    view.rerender(<AccountAISettingsModal account={accountFixture} settings={enabledSettings} agentSupport={agentSupportFixture} saving={false} onChange={onChange} onAgentChange={noopAccountAction} onClose={noopAccountAction} onSave={onSave} />);
    fireEvent.click(screen.getByLabelText('切换 AI 自动改价'));
    expect(onChange).toHaveBeenNthCalledWith(3, { ...enabledSettings, auto_adjust_price_enabled: true });
  });

  test('客服 Agent 分组展示继承态并在选择后写回覆盖值', /* 当前回调验证三态继承与覆盖值往返。 */ () => {
    // onAgentChange 是客服 Agent 授权草稿更新测试替身。
    const onAgentChange = vi.fn();
    const view = render(<AccountAISettingsModal account={accountFixture} settings={aiSettingsFixture} agentSupport={agentSupportFixture} saving={false} onChange={noopAccountAction} onAgentChange={onAgentChange} onClose={noopAccountAction} onSave={noopAccountAction} />);
    // enabledSelect 是启用状态下拉框，两个覆盖值为空时必须落在继承项上并显示继承到的生效值。
    const enabledSelect = screen.getByLabelText('客服 Agent 启用状态') as HTMLSelectElement;
    expect(enabledSelect.value).toBe(INHERIT_AGENT_VALUE);
    expect(screen.getByText('跟随租户默认（当前启用）')).toBeTruthy();
    expect(screen.getByText('跟随租户默认（当前只读）')).toBeTruthy();

    fireEvent.change(enabledSelect, { target: { value: 'off' } });
    expect(onAgentChange).toHaveBeenCalledWith({ enabled: false });
    fireEvent.change(screen.getByLabelText('客服 Agent 能力档位'), { target: { value: 'advanced' } });
    expect(onAgentChange).toHaveBeenCalledWith({ preset: 'advanced' });

    fireEvent.change(screen.getByLabelText('客服 Agent 启用状态'), { target: { value: INHERIT_AGENT_VALUE } });
    expect(onAgentChange).toHaveBeenCalledWith({ enabled: null });
    fireEvent.change(screen.getByLabelText('客服 Agent 能力档位'), { target: { value: INHERIT_AGENT_VALUE } });
    expect(onAgentChange).toHaveBeenCalledWith({ preset: null });

    view.rerender(<AccountAISettingsModal account={accountFixture} settings={aiSettingsFixture} agentSupport={{ ...agentSupportFixture, enabled: true, preset: 'advanced' }} saving={false} onChange={noopAccountAction} onAgentChange={onAgentChange} onClose={noopAccountAction} onSave={noopAccountAction} />);
    expect((screen.getByLabelText('客服 Agent 启用状态') as HTMLSelectElement).value).toBe('on');
    expect((screen.getByLabelText('客服 Agent 能力档位') as HTMLSelectElement).value).toBe('advanced');
  });

  test('客服 Agent 授权读取失败时只提示不可用且不渲染控件', /* 当前回调验证读取失败不退化成一个会被误保存的空配置。 */ () => {
    render(<AccountAISettingsModal account={accountFixture} settings={aiSettingsFixture} agentSupport={null} saving={false} onChange={noopAccountAction} onAgentChange={noopAccountAction} onClose={noopAccountAction} onSave={noopAccountAction} />);
    expect(screen.getByText(/账号级客服 Agent 授权未能读取/)).toBeTruthy();
    expect(screen.queryByLabelText('客服 Agent 启用状态')).toBeNull();
    expect(screen.queryByLabelText('客服 Agent 能力档位')).toBeNull();
  });

  test('删除确认框展示错误并转发确认动作', /* 当前回调验证删除确认框的错误和提交分支。 */ () => {
    // onConfirm 是删除确认测试替身。
    const onConfirm = vi.fn();
    render(<AccountDeleteDialog account={accountFixture} deleting={false} error="删除失败" onClose={noopAccountAction} onConfirm={onConfirm} />);
    expect(screen.getByRole('alert').textContent).toContain('删除失败');
    fireEvent.click(screen.getByText('确认删除'));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  test('二维码弹窗展示风控验证面板且不生成外部链接', /* 当前回调验证二维码风控状态的安全展示边界。 */ () => {
    render(<AccountQRCodeModal target={accountFixture} status="verification" codeUrl="" errorMessage="" faceQrUrl="face-qr" verificationScreenshot="screen" onClose={noopAccountAction} />);
    expect(screen.getByText('需要完成安全风控验证')).toBeTruthy();
    expect(document.querySelectorAll('a')).toHaveLength(0);
  });
});
