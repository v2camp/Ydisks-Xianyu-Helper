import { useCallback,useEffect,useRef,useState } from 'react';
import { getMCPServiceStatus } from './api';
import { getAgentSupportSettings,updateAgentSupportSettings } from './agentSupportApi';
import { createMCPServiceMessage,isSettingsAbortError,settingsErrorMessage } from './state';
import type { AgentSupportSettings,AgentSupportSettingsUpdate,MCPServiceMessage } from './models';

/** Agent 中心页的 Hook 返回值。 */
export interface UseAgentCenterResult {
  /** settings 是租户级客服 Agent 编辑草稿；未加载成功时为 null。 */
  settings: AgentSupportSettings | null;
  /** loading 表示租户配置是否正在加载。 */
  loading: boolean;
  /** loadError 保存租户配置加载失败信息；空串表示无错误。 */
  loadError: string;
  /** saving 表示租户配置是否正在保存。 */
  saving: boolean;
  /** saveMessage 保存最近一次提交的结果提示；null 表示本次进入页面尚未提交过。 */
  saveMessage: MCPServiceMessage | null;
  /** harnessEnabled 表示外部 Harness 依赖的 MCP 服务是否已启用。 */
  harnessEnabled: boolean;
  /** harnessEndpoint 是外部 Harness 的接入地址；未启用或读取失败时为空串。 */
  harnessEndpoint: string;
  /** harnessLoading 表示 Harness 接入状态是否正在加载。 */
  harnessLoading: boolean;
  /** load 重新读取租户级客服 Agent 配置。 */
  load: () => void;
  /** updateDraft 把单字段变化合并进租户配置草稿。 */
  updateDraft: (patch: Partial<AgentSupportSettingsUpdate>) => void;
  /** submit 保存租户配置。 */
  submit: () => Promise<void>;
}

/** 管理 Agent 中心的租户配置草稿、保存状态与外部 Harness 接入现状。 */
export const useAgentCenter = (): UseAgentCenterResult => {
  // settings 保存租户级客服 Agent 编辑草稿；未加载成功时为 null。
  const [settings, setSettings] = useState<AgentSupportSettings | null>(null);
  // loading 表示租户配置是否正在加载。
  const [loading, setLoading] = useState(false);
  // loadError 保存租户配置加载失败信息。
  const [loadError, setLoadError] = useState('');
  // saving 表示租户配置是否正在保存。
  const [saving, setSaving] = useState(false);
  // saveMessage 保存最近一次提交的结果提示。
  const [saveMessage, setSaveMessage] = useState<MCPServiceMessage | null>(null);
  // harnessEnabled 表示 MCP 服务是否已启用。
  const [harnessEnabled, setHarnessEnabled] = useState(false);
  // harnessEndpoint 保存外部 Harness 的接入地址。
  const [harnessEndpoint, setHarnessEndpoint] = useState('');
  // harnessLoading 表示 Harness 接入状态是否正在加载。
  const [harnessLoading, setHarnessLoading] = useState(false);
  // settingsController 保存当前可取消的租户配置请求。
  const settingsController = useRef<AbortController | null>(null);

  /** load 读取租户级客服 Agent 配置，并用取消信号拒绝离开页面后到达的响应。 */
  const load = useCallback(/* 当前回调封装租户配置读取的取消与错误收敛。 */ () => {
    settingsController.current?.abort();
    // controller 是本次租户配置请求的取消控制器。
    const controller = new AbortController();
    settingsController.current = controller;
    setLoading(true);
    setLoadError('');
    void getAgentSupportSettings({ signal: controller.signal })
      .then(/* response 是归一后的租户级客服 Agent 配置。 */ response => {
        if (controller.signal.aborted) return;
        setSettings(response);
      })
      .catch(/* error 是租户配置读取失败或取消原因。 */ error => {
        if (controller.signal.aborted || isSettingsAbortError(error)) return;
        setLoadError(settingsErrorMessage(error, '读取客服 Agent 配置失败'));
      })
      .finally(/* 当前回调在租户配置读取结束后收敛加载标记。 */ () => {
        if (!controller.signal.aborted) setLoading(false);
      });
  }, []);

  // mountEffect 在页面挂载时读取配置并把取消控制器交还给卸载清理。
  useEffect(/* effect 在页面挂载时触发首次读取，并在卸载时释放请求。 */ () => {
    load();
    return /* cleanup 在页面卸载时取消尚未完成的租户配置请求。 */ () => settingsController.current?.abort();
  }, [load]);

  // harnessEffect 只读取一次 Harness 接入现状，失败时保持「未启用」的保守展示。
  useEffect(/* effect 在页面挂载时读取 MCP 服务现状，并在卸载时取消请求。 */ () => {
    // controller 取消页面卸载后不再需要的 MCP 状态请求。
    const controller = new AbortController();
    setHarnessLoading(true);
    void getMCPServiceStatus({ signal: controller.signal })
      .then(/* response 是 MCP 服务的非敏感状态。 */ response => {
        if (controller.signal.aborted) return;
        setHarnessEnabled(response.enabled);
        setHarnessEndpoint(response.endpoint);
      })
      .catch(/* 当前回调吞掉状态读取失败，本栏是只读展示，失败不阻断页面其余部分。 */ () => undefined)
      .finally(/* 当前回调在 MCP 状态读取结束后收敛加载标记。 */ () => {
        if (!controller.signal.aborted) setHarnessLoading(false);
      });
    return /* cleanup 在页面卸载时释放 MCP 状态请求。 */ () => controller.abort();
  }, []);

  /** updateDraft 合并单个字段变化；档位名单始终保留服务端下发的那一份。 */
  const updateDraft = useCallback(/* 当前回调把单字段变化合并进当前草稿。 */ (patch: Partial<AgentSupportSettingsUpdate>) => {
    setSettings(/* updater 基于当前草稿合并补丁，草稿缺失时不重建。 */ current => current ? { ...current, ...patch } : current);
  }, []);

  /** submit 保存租户级客服 Agent 的开关与档位。 */
  const submit = useCallback(/* 当前回调提交租户配置并收敛保存提示。 */ async (): Promise<void> => {
    if (!settings || saving) return;
    setSaving(true);
    setSaveMessage(null);
    try {
      await updateAgentSupportSettings({ enabled: settings.enabled, preset: settings.preset });
      setSaveMessage(createMCPServiceMessage('success', '客服 Agent 配置已保存'));
    } catch (/* error 是保存请求的失败原因，转为页面提示而不改动草稿。 */ error) {
      setSaveMessage(createMCPServiceMessage('error', settingsErrorMessage(error, '保存客服 Agent 配置失败')));
    } finally {
      setSaving(false);
    }
  }, [saving, settings]);

  return {
    settings, loading, loadError, saving, saveMessage,
    harnessEnabled, harnessEndpoint, harnessLoading,
    load, updateDraft, submit,
  };
};
