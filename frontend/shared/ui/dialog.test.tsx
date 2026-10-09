// @vitest-environment jsdom
import { act,cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import { appDialog } from './dialog';
import { DialogHost } from './DialogHost';

// dialogHostFixture 是每个用例复用的居中模态宿主，卸载时会释放仍在等待的请求。
const dialogHostFixture = () => render(<DialogHost />);

describe('appDialog 居中对话框服务', /* 当前回调覆盖降级、回车确认、取消、输入与请求排队。 */ () => {
  beforeEach(/* 当前回调重置浏览器原生弹框替身，避免用例之间互相影响。 */ () => {
    vi.spyOn(window, 'alert').mockImplementation(/* alertImplementation 屏蔽原生提示。 */ () => undefined);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.spyOn(window, 'prompt').mockReturnValue('原生输入');
  });

  afterEach(/* 当前回调卸载宿主并恢复原生 api，保证下个用例从无宿主状态开始。 */ () => {
    cleanup();
    vi.restoreAllMocks();
  });

  test('未挂载宿主时降级为浏览器原生弹框', /* 当前回调验证无渲染宿主时调用方仍能完成交互。 */ async () => {
    await appDialog.alert('原生提示');
    expect(window.alert).toHaveBeenCalledWith('原生提示');

    vi.mocked(window.confirm).mockReturnValue(false);
    await expect(appDialog.confirm('原生确认')).resolves.toBe(false);
    expect(window.confirm).toHaveBeenCalledWith('原生确认');

    await expect(appDialog.prompt('原生输入', '默认值')).resolves.toBe('原生输入');
    expect(window.prompt).toHaveBeenCalledWith('原生输入', '默认值');
  });

  test('挂载宿主后提示以居中模态展示并支持回车关闭', /* 当前回调验证提示类对话框的语义配色与回车确认。 */ async () => {
    dialogHostFixture();
    // closed 记录提示被关闭后调用方的回调次数。
    const closed = vi.fn();
    await act(/* openAlertAction 打开成功语义的提示对话框。 */ async () => {
      void appDialog.alert('商品同步完成', { variant: 'success' }).then(closed);
    });

    // dialog 是当前唯一展示的居中对话框。
    const dialog = screen.getByRole('dialog');
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    expect(screen.getByText('操作成功')).toBeTruthy();
    expect(screen.getByText('商品同步完成')).toBeTruthy();
    expect(window.alert).not.toHaveBeenCalled();

    fireEvent.keyDown(dialog, { key: 'Enter' });
    await waitFor(/* closeAssertion 等待提示关闭并触发调用方回调。 */ () => expect(closed).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  test('确认框支持点击主按钮确认与点击遮罩取消', /* 当前回调验证两类点击路径分别回传真值与假值。 */ async () => {
    dialogHostFixture();
    // onConfirm 记录确认结果。
    const onConfirm = vi.fn();
    await act(/* openConfirmAction 打开危险语义的删除确认框。 */ async () => {
      void appDialog.confirm('确认删除该卡密吗？', { variant: 'danger', confirmText: '删除' }).then(onConfirm);
    });
    fireEvent.click(screen.getByRole('button', { name: '删除' }));
    await waitFor(/* confirmAssertion 等待确认结果回传真值。 */ () => expect(onConfirm).toHaveBeenCalledWith(true));

    // onCancel 记录遮罩取消结果。
    const onCancel = vi.fn();
    await act(/* openMaskAction 打开另一个确认框用于遮罩关闭。 */ async () => {
      void appDialog.confirm('确认删除该订单吗？').then(onCancel);
    });
    fireEvent.mouseDown(screen.getByRole('dialog').parentElement as Element);
    await waitFor(/* cancelAssertion 等待遮罩点击回传假值。 */ () => expect(onCancel).toHaveBeenCalledWith(false));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  test('确认框的取消按钮与 Escape 键都回传假值', /* 当前回调验证键盘与按钮两条取消路径。 */ async () => {
    dialogHostFixture();
    // onClickCancel 记录取消按钮结果。
    const onClickCancel = vi.fn();
    await act(/* openButtonAction 打开待取消的确认框。 */ async () => {
      void appDialog.confirm('确认删除该自动化规则吗？').then(onClickCancel);
    });
    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    await waitFor(/* buttonCancelAssertion 等待取消结果回传假值。 */ () => expect(onClickCancel).toHaveBeenCalledWith(false));

    // onEscapeCancel 记录 Escape 取消结果。
    const onEscapeCancel = vi.fn();
    await act(/* openEscapeAction 打开待用 Escape 关闭的确认框。 */ async () => {
      void appDialog.confirm('确认清空该账号的默认回复记录吗？').then(onEscapeCancel);
    });
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    await waitFor(/* escapeCancelAssertion 等待 Escape 回传假值。 */ () => expect(onEscapeCancel).toHaveBeenCalledWith(false));
  });

  test('输入框回车提交当前文本且 Escape 回传空值', /* 当前回调验证输入型对话框的提交与取消。 */ async () => {
    dialogHostFixture();
    // onSubmitted 记录输入提交结果。
    const onSubmitted = vi.fn();
    await act(/* openPromptAction 打开带默认值的输入对话框。 */ async () => {
      void appDialog.prompt('复制卡密组ID', '1001', { variant: 'warning' }).then(onSubmitted);
    });
    expect(screen.getByDisplayValue('1001')).toBeTruthy();
    // input 是输入型对话框的文本框。
    const input = screen.getByRole('textbox');
    fireEvent.change(input, { target: { value: '2002' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(/* submitAssertion 等待输入文本回传。 */ () => expect(onSubmitted).toHaveBeenCalledWith('2002'));

    // onDismissed 记录输入取消结果。
    const onDismissed = vi.fn();
    await act(/* openDismissAction 打开待取消的输入对话框。 */ async () => {
      void appDialog.prompt('复制卡密组ID').then(onDismissed);
    });
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    await waitFor(/* dismissAssertion 等待取消回传空值。 */ () => expect(onDismissed).toHaveBeenCalledWith(null));
  });

  test('连续请求按到达顺序逐条展示', /* 当前回调验证队列化展示不会让后发请求抢占先发请求。 */ async () => {
    dialogHostFixture();
    // order 记录两个请求的完成顺序。
    const order: string[] = [];
    await act(/* openQueueAction 连续打开两个提示对话框。 */ async () => {
      void appDialog.alert('第一条', { variant: 'info', title: '排队一' }).then(/* firstResolve 记录第一条完成。 */ () => order.push('first'));
      void appDialog.alert('第二条', { variant: 'info', title: '排队二' }).then(/* secondResolve 记录第二条完成。 */ () => order.push('second'));
    });
    expect(screen.getByText('排队一')).toBeTruthy();
    expect(screen.queryByText('排队二')).toBeNull();

    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Enter' });
    await waitFor(/* nextAssertion 等待队首关闭后展示第二条。 */ () => expect(screen.getByText('排队二')).toBeTruthy());
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Enter' });
    await waitFor(/* doneAssertion 等待两个请求全部完成。 */ () => expect(order).toEqual(['first', 'second']));
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
