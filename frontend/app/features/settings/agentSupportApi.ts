import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { collectionFrom } from '../../../shared/http/contract';
import { type RequestControlOptions } from '../../../shared/http/client';
import type { AgentPresetOption, AgentSupportSettings, AgentSupportSettingsUpdate } from './models';

// parsePresetOptions 把服务端下发的档位名单归一为 UI 模型；缺少稳定标识的条目无法参与保存，直接丢弃。
const parsePresetOptions = (payload: unknown): AgentPresetOption[] => collectionFrom<Record<string, unknown>>(payload, ['presets'])
  .filter(/* entry 是当前待校验的档位条目，value 缺失或为空串的条目一律不作候选。 */ entry => typeof entry?.value === 'string' && entry.value !== '')
  .map(/* entry 是当前档位条目，逐字段归一为可供界面渲染的选项模型。 */ entry => ({
    value: String(entry.value),
    label: String(entry.label ?? ''),
    description: String(entry.description ?? ''),
  }));

/** 读取租户级客服 Agent 的默认开关、默认档位与服务端下发的可选档位名单。 */
export const getAgentSupportSettings = async (options?: RequestControlOptions): Promise<AgentSupportSettings> => {
  // response 是 OpenAPI 约束的客服 Agent 租户配置响应。
  const response = await runContractRequest(/* signal 控制客服 Agent 租户配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/agent/support/settings', { signal }), options);
  return {
    enabled: response.enabled === true,
    preset: typeof response.preset === 'string' ? response.preset : '',
    presets: parsePresetOptions(response.presets),
  };
};

/** 保存租户级客服 Agent 的默认开关与档位；该配置是未单独覆盖的账号的继承来源。 */
export const updateAgentSupportSettings = async (update: AgentSupportSettingsUpdate, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制客服 Agent 租户配置保存的取消和超时。 */ signal => contractClient.PUT('/api/v1/agent/support/settings', {
    body: { enabled: update.enabled, preset: update.preset },
    signal,
  }), options);
};
