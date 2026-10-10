import { Bot,Loader2,Radio,RefreshCw,Save,Terminal } from 'lucide-react';
import React from 'react';
import { useAgentCenter } from '../agentCenterHooks';

/** AgentCenter 按「谁决定调用能力」归拢三类 Agent 的入口，并直接配置租户级客服 Agent 的默认开关与档位。 */
const AgentCenter: React.FC = () => {
  // center 是本页的配置草稿、保存状态与外部 Harness 接入现状。
  const {
    settings, loading, loadError, saving, saveMessage,
    harnessEnabled, harnessEndpoint, harnessLoading,
    load, updateDraft, submit,
  } = useAgentCenter();

  return (
    <div className="max-w-6xl mx-auto space-y-8 animate-fade-in pb-24">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="w-12 h-12 bg-gray-100 rounded-2xl flex items-center justify-center">
            <Bot className="w-6 h-6 text-gray-600" />
          </div>
          <div>
            <h2 className="text-3xl font-extrabold text-gray-900">Agent 中心</h2>
            <p className="text-gray-500 mt-1 text-sm font-medium">三类 Agent 各自的入口与当前生效状态</p>
          </div>
        </div>
        <button type="button" onClick={load} className="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-xl font-bold text-gray-700 flex items-center gap-2 transition-colors">
          <RefreshCw className="w-4 h-4" />
          刷新
        </button>
      </div>

      {/* 管理端 Agent：外部 Harness 由人在场操作，凭管理员身份直接调用工具，权限与工具范围都不设档位。 */}
      <section className="space-y-4">
        <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
          <div className="p-1.5 rounded-lg bg-slate-700 text-white">
            <Terminal className="w-4 h-4" />
          </div>
          管理端 Agent
        </h3>
        <div className="ios-card rounded-xl p-6 bg-white space-y-4">
          <div className="flex items-center gap-2">
            <h4 className="text-sm font-bold text-gray-800">外部 Harness 接入（MCP）</h4>
            <span className={`text-[11px] font-bold px-2 py-0.5 rounded-full ${harnessEnabled ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-500'}`}>
              {harnessLoading ? '读取中' : harnessEnabled ? '已启用' : '未启用'}
            </span>
          </div>
          <p className="text-xs leading-5 text-gray-600">
            外部 Harness 由人在场操作，通过 MCP 协议以管理员身份调用本服务工具，能力上限就是平台自身的操作范围，
            因此这里没有档位可配。它的启用开关、接入令牌与调用审计在「系统设置」页的 MCP 服务卡片中管理。
          </p>
          {harnessEndpoint && (
            <p className="text-xs text-gray-500 font-mono break-all">接入地址：{harnessEndpoint}</p>
          )}
        </div>
      </section>

      {/* 客服端 Agent：无人监督、作用域锁定单个账号，因此能力由档位统一约束，不逐个工具放行。 */}
      <section className="space-y-4">
        <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
          <div className="p-1.5 rounded-lg bg-sky-500 text-white">
            <Bot className="w-4 h-4" />
          </div>
          客服端 Agent
        </h3>
        <div className="ios-card rounded-xl p-6 bg-white space-y-5">
          {!settings ? (
            <div className="p-6 text-center text-gray-400 space-y-3">
              {loadError || (loading ? '加载配置中...' : '暂无配置')}
              {!loading && loadError && (
                <div>
                  <button type="button" className="ios-btn-primary px-4 py-2 rounded-xl" onClick={load}>重新加载</button>
                </div>
              )}
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between p-4 bg-sky-50 rounded-xl">
                <div>
                  <div className="font-bold text-gray-900">启用客服 Agent</div>
                  <div className="text-xs text-gray-500">开启后客服 Agent 可自主查询并答复买家；未开启时卖家账号只走既有的规则与 AI 回复通道</div>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={settings.enabled}
                  aria-label="启用客服 Agent"
                  onClick={/* 切换租户默认开关草稿。 */ () => updateDraft({ enabled: !settings.enabled })}
                  className={`w-14 h-8 rounded-full transition-colors duration-300 relative flex-shrink-0 ${settings.enabled ? 'bg-brand' : 'bg-gray-300'}`}
                >
                  <span className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow-md transition-transform duration-300 ${settings.enabled ? 'translate-x-6' : 'translate-x-0'}`} />
                </button>
              </div>

              <div className="space-y-3">
                <div className="text-sm font-bold text-gray-800">能力档位</div>
                <div className="grid gap-3 md:grid-cols-3">
                  {settings.presets.map(/* option 是服务端下发的当前档位选项。 */ option => (
                    <button
                      key={option.value}
                      type="button"
                      aria-pressed={settings.preset === option.value}
                      onClick={/* 选中当前档位并写回草稿。 */ () => updateDraft({ preset: option.value })}
                      className={`rounded-xl border p-4 text-left transition-colors ${settings.preset === option.value ? 'border-sky-400 bg-sky-50' : 'border-gray-200 hover:border-gray-300'}`}
                    >
                      <span className="block text-sm font-bold text-gray-900">{option.label}</span>
                      <span className="mt-1 block text-xs leading-5 text-gray-600">{option.description}</span>
                    </button>
                  ))}
                </div>
                {settings.presets.length === 0 && (
                  <p className="text-xs text-amber-700">服务端未下发档位名单，暂时无法切换档位，请确认服务端版本与本页面匹配。</p>
                )}
                <p className="text-xs leading-5 text-gray-500">
                  这里设置的是租户默认档位。单个卖家账号可在账号列表里单独覆盖，未覆盖的账号按此默认生效。
                  档位与能力内核一一对应，界面不另外维护一份工具清单。
                </p>
              </div>

              <div className="flex items-center justify-between gap-4 border-t border-gray-100 pt-5">
                <span className={`text-xs ${saveMessage?.type === 'error' ? 'text-red-600' : 'text-gray-500'}`}>
                  {saveMessage?.text || '账号未单独覆盖时，按这里的默认值生效'}
                </span>
                <button type="button" onClick={/* 提交租户默认开关与档位。 */ () => void submit()} disabled={saving} className="ios-btn-primary px-6 py-3 rounded-xl font-bold flex items-center gap-2 disabled:opacity-70">
                  {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                  {saving ? '保存中...' : '保存'}
                </button>
              </div>
            </>
          )}
        </div>
      </section>

      {/* 运营 Agent：启用前提是 QQ 机器人凭据已配置，配置项就地留在 QQ 连接器卡片，本页只作指引。 */}
      <section className="space-y-4">
        <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
          <div className="p-1.5 rounded-lg bg-violet-500 text-white">
            <Radio className="w-4 h-4" />
          </div>
          运营 Agent
        </h3>
        <div className="ios-card rounded-xl p-6 bg-white space-y-3">
          <p className="text-sm leading-6 text-gray-700">
            运营 Agent 走 QQ 通道，由管理员在 QQ 里直接下发指令，它的启用前提是 QQ 机器人凭据已经配置好。
            因此开关与命令发送者白名单留在「系统设置」页的 QQ 机器人连接器卡片里，不迁到本页。
          </p>
          <p className="text-xs leading-5 text-gray-500">
            放在一起会出现「在 Agent 中心打开了、却因为 QQ 没配好而毫无反应」的困惑。
            本页不对该 Agent 提供配置项。
          </p>
        </div>
      </section>
    </div>
  );
};

export default AgentCenter;
