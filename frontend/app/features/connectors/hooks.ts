import { useCallback,useEffect,useRef,useState } from 'react';
import type { ChannelRequestOptions,MutationIDResponse,NotificationChannel,NotificationChannelEditorResponse,NotificationChannelRequest,OperationResponse } from './api';
import { createNotificationChannel,createSystemAlertChannel,deleteNotificationChannel,deleteSystemAlertChannel,getNotificationChannel,getNotificationChannels,getSystemAlertChannel,getSystemAlertChannels,testNotificationChannel,testSystemAlertChannel,updateNotificationChannel,updateSystemAlertChannel } from './api';
import { buildNotificationPayload,emptyNotificationForm,isCurrentNotificationRequest,normalizeNotificationForm,notificationErrorMessage,validateNotificationForm } from './state';
import type { ConnectorsState,NotificationForm } from './types';
import { appDialog } from '../../../shared/ui/dialog';

/** ChannelAdminAPI 定义渠道 CRUD 的传输适配器；租户渠道与系统告警渠道各绑一套实现。 */
export interface ChannelAdminAPI {
  /** 读取渠道列表。 */
  list: (options?: ChannelRequestOptions) => Promise<{ /** 操作是否完成。 */ success: boolean; /** 渠道列表。 */ data: NotificationChannel[] }>;
  /** 读取单条渠道的脱敏编辑配置。 */
  get: (channelID: string, options?: ChannelRequestOptions) => Promise<NotificationChannelEditorResponse>;
  /** 创建渠道。 */
  create: (data: Required<Pick<NotificationChannelRequest, 'name' | 'type' | 'config'>> & NotificationChannelRequest, options?: ChannelRequestOptions) => Promise<MutationIDResponse>;
  /** 更新渠道。 */
  update: (channelID: string, data: NotificationChannelRequest, options?: ChannelRequestOptions) => Promise<OperationResponse>;
  /** 删除渠道。 */
  remove: (channelID: string, options?: ChannelRequestOptions) => Promise<OperationResponse>;
  /** 发送测试通知。 */
  test: (channelID: string, options?: ChannelRequestOptions) => Promise<OperationResponse>;
}

// tenantChannelAPI 把租户渠道传输函数绑定为渠道管理适配器。
const tenantChannelAPI: ChannelAdminAPI = {
  list: getNotificationChannels,
  get: getNotificationChannel,
  create: createNotificationChannel,
  update: updateNotificationChannel,
  remove: deleteNotificationChannel,
  test: testNotificationChannel,
};

// systemAlertChannelAPI 把系统告警渠道传输函数绑定为渠道管理适配器。
const systemAlertChannelAPI: ChannelAdminAPI = {
  list: getSystemAlertChannels,
  get: getSystemAlertChannel,
  create: createSystemAlertChannel,
  update: updateSystemAlertChannel,
  remove: deleteSystemAlertChannel,
  test: testSystemAlertChannel,
};

// useChannelAdmin 统一管理一套渠道的列表、编辑表单与渠道动作异步状态。
const useChannelAdmin = (api: ChannelAdminAPI): ConnectorsState => {
  // channels 保存当前用户的通知渠道。
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  // loading 表示通知渠道列表是否正在加载。
  const [loading, setLoading] = useState(true);
  // showModal 表示渠道新建或编辑弹窗是否打开。
  const [showModal, setShowModal] = useState(false);
  // editing 保存当前正在编辑的渠道。
  const [editing, setEditing] = useState<NotificationChannel | null>(null);
  // savedCredentialKeys 保存当前编辑渠道已保存的凭据键名，只含键名不含值，供弹窗提示「留空则保持原值」。
  const [savedCredentialKeys, setSavedCredentialKeys] = useState<string[]>([]);
  // saving 表示渠道保存动作是否正在执行。
  const [saving, setSaving] = useState(false);
  // testingId 保存当前正在测试发送的渠道 ID。
  const [testingId, setTestingId] = useState('');
  // toast 保存当前短暂操作提示。
  const [toast, setToast] = useState<ConnectorsState['toast']>(null);
  // form 保存渠道编辑弹窗的当前表单。
  const [form, setForm] = useState<NotificationForm>(emptyNotificationForm);
  // channelGeneration 隔离渠道加载和刷新产生的旧响应。
  const channelGeneration = useRef(0);
  // channelAbort 保存当前渠道列表请求的取消控制器。
  const channelAbort = useRef<AbortController | null>(null);
  // actionGeneration 隔离保存、删除和测试动作的旧响应。
  const actionGeneration = useRef(0);
  // actionAbort 保存当前渠道变更动作的取消控制器。
  const actionAbort = useRef<AbortController | null>(null);
  // toastTimer 保存提示自动消失的定时器句柄。
  const toastTimer = useRef<number | null>(null);
  // editorGeneration 隔离连续打开不同渠道时产生的旧编辑配置响应。
  const editorGeneration = useRef(0);
  // editorAbort 保存当前编辑配置请求的取消控制器。
  const editorAbort = useRef<AbortController | null>(null);

  // showToast 展示短暂提示并清理上一条提示的定时器。
  const showToast = useCallback(
    // 提示回调写入提示状态并安排自动清理。
    (type: NonNullable<ConnectorsState['toast']>['type'], text: string) => {
      setToast({ type, text });
      if (toastTimer.current !== null) window.clearTimeout(toastTimer.current);
      toastTimer.current = window.setTimeout(
        // toastCleanup 在提示展示三秒后清理状态。
        () => {
          toastTimer.current = null;
          setToast(null);
        },
        3000,
      );
    },
    [],
  );

  // loadChannels 加载通知渠道并丢弃过期响应。
  const loadChannels = useCallback(
    // 渠道加载回调读取最新通知渠道列表。
    async () => {
      // generation 标记本次渠道查询的代次。
      const generation = ++channelGeneration.current;
      channelAbort.current?.abort();
      // controller 允许刷新或卸载时取消渠道请求。
      const controller = new AbortController();
      channelAbort.current = controller;
      setLoading(true);
      try {
        // result 是当前用户的通知渠道列表响应。
        const result = await api.list({ signal: controller.signal });
        if (!isCurrentNotificationRequest(generation, channelGeneration.current)) return;
        setChannels(result.data || []);
      } catch (error: unknown /* 渠道加载异常 */) {
        if (isCurrentNotificationRequest(generation, channelGeneration.current) && !controller.signal.aborted) {
          console.error('加载通知渠道失败', error);
        }
      } finally {
        if (isCurrentNotificationRequest(generation, channelGeneration.current)) setLoading(false);
      }
    },
    [api],
  );

  useEffect(
    // 通知数据副作用在挂载时加载渠道，卸载时取消未完成请求。
    () => {
      void loadChannels();
      return (
        // 通知数据清理回调取消渠道请求并使编辑读取失效。
        () => {
          channelGeneration.current += 1;
          channelAbort.current?.abort();
          editorGeneration.current += 1;
          editorAbort.current?.abort();
        }
      );
    },
    [loadChannels],
  );

  useEffect(
    // 卸载清理负责终止渠道动作的展示生命周期并释放提示定时器。
    () => (
      // toastCleanup 在页面卸载时取消渠道动作、使旧结果失效并释放定时器。
      () => {
        actionGeneration.current += 1;
        actionAbort.current?.abort();
        if (toastTimer.current !== null) window.clearTimeout(toastTimer.current);
      }
    ),
    [],
  );

  // resetCredentialState 清空已保存凭据键与清除意图；渠道类型切换后不得沿用上一类型的凭据状态。
  const resetCredentialState = useCallback(
    // 重置回调清空凭据键集合，并把表单的清除意图还原为缺省（保存即保留）。
    () => {
      setSavedCredentialKeys([]);
      setForm(
        // nextFormUpdater 只清除清除意图，其余表单值保持不变。
        current => ({ ...current, credentialAction: undefined }),
      );
    },
    [],
  );

  // openCreate 打开新建渠道表单并清理上一次编辑状态。
  const openCreate = useCallback(
    // 新建回调重置表单并打开渠道弹窗。
    () => {
      editorGeneration.current += 1;
      editorAbort.current?.abort();
      setEditing(null);
      setSavedCredentialKeys([]);
      setForm(emptyNotificationForm());
      setShowModal(true);
    },
    [],
  );

  // openEdit 先读取脱敏编辑配置再开放表单；关闭、切换或新建使旧请求失效，避免晚到响应覆盖输入。
  const openEdit = useCallback(
    // channel 是本次编辑对象；加载期间关闭旧表单，只有仍属于当前代次的响应能打开新表单。
    async (channel: NotificationChannel) => {
      // generation 标记本次编辑加载，隔离取消与连续打开不同渠道的结果。
      const generation = ++editorGeneration.current;
      editorAbort.current?.abort();
      // controller 控制当前读取的取消，不参与保存请求的生命周期。
      const controller = new AbortController();
      editorAbort.current = controller;
      setShowModal(false);
      try {
        // editor 只含可回显的非敏感配置与已保存的凭据键名，凭据明文始终留在服务端。
        const editor = await api.get(channel.id, { signal: controller.signal });
        if (generation !== editorGeneration.current || controller.signal.aborted) return;
        // initialForm 是读取完成后一次性建立的表单，之后不再由该请求回填。
        const initialForm = normalizeNotificationForm(channel);
        setEditing(channel);
        setSavedCredentialKeys(editor.saved_credential_keys ?? []);
        setForm({
          ...initialForm,
          credentialAction: undefined,
          // 非邮件渠道回填服务端可安全回显的配置；邮件渠道只回填收件地址，敏感凭据键留空保存即沿用服务端原值。
          config: {
            ...initialForm.config,
            ...(editor.config ?? {}),
            ...(editor.type === 'email' && editor.to_email ? { to_email: editor.to_email } : {}),
          },
        });
        setShowModal(true);
      } catch (error: unknown /* 编辑读取失败时仅显示可读错误，不开放缺少配置的表单。 */) {
        if (generation === editorGeneration.current && !controller.signal.aborted) showToast('error', notificationErrorMessage(error, '读取渠道配置失败'));
      }
    },
    [api, showToast],
  );

  // closeModal 关闭渠道弹窗并取消未完成的保存动作。
  const closeModal = useCallback(
    // 关闭回调使保存请求失效并隐藏弹窗。
    () => {
      actionGeneration.current += 1;
      setTestingId('');
      actionAbort.current?.abort();
      editorGeneration.current += 1;
      editorAbort.current?.abort();
      setSaving(false);
      setShowModal(false);
      setSavedCredentialKeys([]);
    },
    [],
  );

  // handleSave 校验并保存当前渠道表单，失败时保留表单供重试。
  const handleSave = useCallback(
    // 保存回调执行渠道校验、请求和成功刷新。
    async () => {
      // validationError 是渠道表单预检失败时的用户提示；已保存凭据可留空沿用。
      const validationError = validateNotificationForm(form, { savedCredentialKeys });
      if (validationError) {
        showToast('error', validationError);
        return;
      }
      // generation 标记当前渠道保存动作的代次。
      const generation = ++actionGeneration.current;
      // 替换动作时主动清理被取消测试的忙碌状态；旧 finally 已没有当前代次。
      setTestingId('');
      actionAbort.current?.abort();
      // controller 允许关闭弹窗时取消保存请求。
      const controller = new AbortController();
      actionAbort.current = controller;
      setSaving(true);
      try {
        // payload 是后端渠道接口使用的规范化请求体，邮件渠道只含收件地址。
        const payload = buildNotificationPayload(form);
        if (editing) {
          await api.update(editing.id, payload, { signal: controller.signal });
        } else {
          await api.create({ ...payload, config: payload.config || {} }, { signal: controller.signal });
        }
        if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
        setShowModal(false);
        await loadChannels();
        if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
        showToast('success', editing ? '已更新' : '已创建');
      } catch (error: unknown /* 渠道保存异常 */) {
        if (isCurrentNotificationRequest(generation, actionGeneration.current) && !controller.signal.aborted) {
          showToast('error', notificationErrorMessage(error, '保存失败'));
        }
      } finally {
        if (isCurrentNotificationRequest(generation, actionGeneration.current)) setSaving(false);
      }
    },
    [api, editing, form, loadChannels, savedCredentialKeys, showToast],
  );

  // handleDelete 删除渠道并在成功后刷新列表。
  const handleDelete = useCallback(
    // 删除回调执行渠道删除请求并刷新列表。
    async (channel: NotificationChannel) => {
      if (!(await appDialog.confirm(`确认删除渠道「${channel.name}」吗？`, { variant: 'danger', confirmText: '删除' }))) return;
      // generation 标记当前删除动作的代次。
      const generation = ++actionGeneration.current;
      // 替换动作时主动清理被取消测试的忙碌状态；旧 finally 已没有当前代次。
      setTestingId('');
      actionAbort.current?.abort();
      // controller 允许刷新页面时取消删除请求。
      const controller = new AbortController();
      actionAbort.current = controller;
      try {
        await api.remove(channel.id, { signal: controller.signal });
        if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
        await loadChannels();
        if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
        showToast('success', '已删除');
      } catch (error: unknown /* 渠道删除异常 */) {
        if (isCurrentNotificationRequest(generation, actionGeneration.current) && !controller.signal.aborted) {
          showToast('error', notificationErrorMessage(error, '删除失败'));
        }
      }
    },
    [api, loadChannels, showToast],
  );

  // handleToggleEnabled 更新渠道启用状态并采用函数式局部更新。
  const handleToggleEnabled = useCallback(
    // 启用切换回调更新渠道状态并保留最新响应。
    async (channel: NotificationChannel) => {
      // generation 标记当前启用状态动作的代次。
      const generation = ++actionGeneration.current;
      // 替换动作时主动清理被取消测试的忙碌状态；旧 finally 已没有当前代次。
      setTestingId('');
      actionAbort.current?.abort();
      // controller 允许新的渠道动作取消旧请求。
      const controller = new AbortController();
      actionAbort.current = controller;
      try {
        await api.update(channel.id, { enabled: !channel.enabled }, { signal: controller.signal });
        if (!isCurrentNotificationRequest(generation, actionGeneration.current)) return;
        setChannels(
          // nextChannelsUpdater 使用函数式更新渠道列表。
          current => current.map(
            // item 是当前待更新的渠道项。
            item => item.id === channel.id ? { ...item, enabled: !item.enabled } : item,
          ),
        );
      } catch (error: unknown /* 渠道状态异常 */) {
        if (isCurrentNotificationRequest(generation, actionGeneration.current) && !controller.signal.aborted) {
          showToast('error', notificationErrorMessage(error, '切换失败'));
        }
      }
    },
    [api, showToast],
  );

  // handleTest 发送渠道测试通知并展示结果。
  const handleTest = useCallback(
    // 测试回调发送测试通知并显示结果。
    async (channel: NotificationChannel) => {
      setTestingId(channel.id);
      // generation 标记当前测试通知动作的代次。
      const generation = ++actionGeneration.current;
      actionAbort.current?.abort();
      // controller 允许新动作取消旧的测试发送。
      const controller = new AbortController();
      actionAbort.current = controller;
      try {
        await api.test(channel.id, { signal: controller.signal });
        if (isCurrentNotificationRequest(generation, actionGeneration.current)) showToast('success', '测试通知已发送，请检查对应渠道');
      } catch (error: unknown /* 测试通知异常 */) {
        if (isCurrentNotificationRequest(generation, actionGeneration.current) && !controller.signal.aborted) {
          showToast('error', notificationErrorMessage(error, '发送失败'));
        }
      } finally {
        if (isCurrentNotificationRequest(generation, actionGeneration.current)) setTestingId('');
      }
    },
    [api, showToast],
  );

  return {
    channels, loading, showModal, editing, saving, testingId, toast,
    savedCredentialKeys, form, setForm,
    loadChannels, openCreate, openEdit, closeModal, resetCredentialState, showToast,
    handleSave, handleDelete, handleToggleEnabled, handleTest,
  };
};

// useConnectors 管理当前用户的租户通知渠道。
export const useConnectors = (): ConnectorsState => useChannelAdmin(tenantChannelAPI);

// useSystemAlertChannels 管理管理员共享的系统告警渠道。
export const useSystemAlertChannels = (): ConnectorsState => useChannelAdmin(systemAlertChannelAPI);
