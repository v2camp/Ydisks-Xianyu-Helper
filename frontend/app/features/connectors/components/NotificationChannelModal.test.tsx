// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,describe,expect,test,vi } from 'vitest';
import type { CredentialSource,NotificationForm } from '../types';
import { NotificationChannelModal } from './NotificationChannelModal';

afterEach(/* cleanupDom 隔离每个用例创建的弹窗 DOM。 */ () => cleanup());

// qqForm 是 qq 渠道弹窗使用的最小表单，凭据字段由各场景按需填入。
const qqForm: NotificationForm = { name: 'QQ 通知', type: 'qq', enabled: true, config: { user_openid: 'user-openid' }, event_types: [] };
// barkForm 是 bark 渠道弹窗使用的最小表单，device_key 是按已保存凭据留空的字段。
const barkForm: NotificationForm = { name: 'Bark', type: 'bark', enabled: true, config: { server_url: 'https://api.day.app' }, event_types: [] };
// dingtalkForm 是钉钉渠道弹窗使用的最小表单，两个必填字段都是凭据。
const dingtalkForm: NotificationForm = { name: '钉钉', type: 'dingtalk', enabled: true, config: {}, event_types: [] };

// ModalCredentialProps 是各场景下弹窗的凭据入参。
interface ModalCredentialProps {
  // credentialSource 表示凭据由哪个已就绪连接器接管。
  credentialSource?: CredentialSource | null;
  // savedCredentialKeys 表示当前渠道已保存的凭据键名。
  savedCredentialKeys?: string[];
}

// renderModal 渲染渠道弹窗并返回表单更新与重置替身，供断言用户操作写回的状态。
const renderModal = (form: NotificationForm, props: ModalCredentialProps = {}) => {
  // setForm 是记录弹窗写回表单的替身；React 未重渲染时测试直接调用其更新函数。
  const setForm = vi.fn();
  // resetCredentialState 是记录类型切换重置调用的替身。
  const resetCredentialState = vi.fn();
  render(
    <NotificationChannelModal
      showModal
      editing={null}
      form={form}
      setForm={setForm}
      smtp={{}}
      showChannelSmtpPassword={false}
      setShowChannelSmtpPassword={vi.fn()}
      saving={false}
      credentialSource={props.credentialSource}
      savedCredentialKeys={props.savedCredentialKeys}
      resetCredentialState={resetCredentialState}
      onClose={vi.fn()}
      onSave={vi.fn()}
    />,
  );
  return { setForm, resetCredentialState };
};

// findField 按配置字段键读取弹窗中的输入框。
const findField = (key: string): HTMLInputElement | null => document.querySelector(`input[data-field="${key}"]`);

// applyUpdater 取出替身收到的第一个表单更新函数并应用，得到弹窗期望写回的表单。
const applyUpdater = (setForm: ReturnType<typeof vi.fn>, current: NotificationForm, callIndex = 0): NotificationForm => (setForm.mock.calls[callIndex][0] as (form: NotificationForm) => NotificationForm)(current);

describe('NotificationChannelModal 的凭据来源', /* 当前测试组验证连接器接管、已保存凭据与自填三种表单形态。 */ () => {
  test('连接器就绪时隐藏凭据字段并按连接器名说明来源', /* readyCase 验证字段隐藏与勾选后的清除意图。 */ () => {
    // setForm 是连接器就绪场景下记录表单写回的替身。
    const { setForm } = renderModal(qqForm, {
      credentialSource: { hiddenKeys: ['app_id', 'app_secret'], connectorName: 'QQ 机器人' },
      savedCredentialKeys: ['app_id', 'app_secret'],
    });
    expect(findField('app_id')).toBeNull();
    expect(findField('app_secret')).toBeNull();
    expect(screen.getByText('机器人 AppID / 机器人 AppSecret 使用「连接器 → QQ 机器人」的系统级凭据。')).toBeTruthy();
    expect(screen.getByText('当前渠道仍保存着独立凭据，继续保存将沿用它们。')).toBeTruthy();
    // 未勾选时表单不带清除意图，保存即沿用渠道现有凭据。
    expect(qqForm.credentialAction).toBeUndefined();

    fireEvent.click(screen.getByRole('checkbox'));
    expect(applyUpdater(setForm, qqForm).credentialAction).toBe('clear');
  });

  test('连接器未就绪但渠道有已保存凭据时字段保留且不再必填', /* optionalCase 验证留空沿用已保存凭据的提示。 */ () => {
    renderModal(qqForm, { credentialSource: null, savedCredentialKeys: ['app_id', 'app_secret'] });
    expect(findField('app_id')?.placeholder).toBe('已保存，留空则保持原值');
    expect(findField('app_secret')?.placeholder).toBe('已保存，留空则保持原值');
    expect(screen.queryByText('当前渠道仍保存着独立凭据，继续保存将沿用它们。')).toBeNull();
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  test('连接器未就绪且渠道无凭据时维持原有必填提示', /* requiredCase 验证默认场景未被新逻辑放宽。 */ () => {
    renderModal(qqForm, { credentialSource: null, savedCredentialKeys: [] });
    expect(findField('app_id')?.placeholder).toBe('你的 QQ 机器人 AppID');
    expect(findField('app_secret')?.placeholder).toBe('QQ 机器人 AppSecret');
    expect(screen.getAllByText('*')).toHaveLength(2);
  });

  test('bark 只把已保存的 device_key 置为可留空', /* barkCase 验证非敏感回显字段仍按必填展示。 */ () => {
    renderModal(barkForm, { credentialSource: null, savedCredentialKeys: ['device_key'] });
    expect(findField('server_url')?.value).toBe('https://api.day.app');
    expect(findField('server_url')?.placeholder).toBe('https://api.day.app');
    expect(findField('device_key')?.placeholder).toBe('已保存，留空则保持原值');
    // 只有 server_url 还带必填星号，device_key 已由服务端保存值兜底。
    expect(screen.getAllByText('*')).toHaveLength(1);
  });

  test('钉钉的加签密钥按密码框渲染且已保存时可留空', /* dingtalkCase 验证密码控件与全凭据渠道的可选提示。 */ () => {
    renderModal(dingtalkForm, { credentialSource: null, savedCredentialKeys: ['secret', 'webhook_url'] });
    expect(findField('webhook_url')?.placeholder).toBe('已保存，留空则保持原值');
    expect(findField('secret')?.type).toBe('password');
    expect(findField('secret')?.placeholder).toBe('已保存，留空则保持原值');
    expect(screen.queryAllByText('*')).toHaveLength(0);
  });

  test('切换渠道类型时清空上一类型的凭据状态', /* typeSwitchCase 验证类型切换触发的重置。 */ () => {
    // resetCredentialState 是类型切换场景记录重置调用的替身。
    const { setForm, resetCredentialState } = renderModal(qqForm, { savedCredentialKeys: ['app_id', 'app_secret'] });
    fireEvent.click(document.querySelector('button[data-type="bark"]') as HTMLButtonElement);
    expect(resetCredentialState).toHaveBeenCalledTimes(1);
    expect(applyUpdater(setForm, qqForm)).toMatchObject({ type: 'bark', config: {} });
  });

  test('重复点击当前渠道类型不重置凭据状态', /* sameTypeCase 验证误触已选类型不会丢弃已填配置。 */ () => {
    // resetCredentialState 是重复点击场景记录重置调用的替身。
    const { resetCredentialState } = renderModal(barkForm, { savedCredentialKeys: ['device_key'] });
    fireEvent.click(document.querySelector('button[data-type="bark"]') as HTMLButtonElement);
    expect(resetCredentialState).not.toHaveBeenCalled();
  });
});
