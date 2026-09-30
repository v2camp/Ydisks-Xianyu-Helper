import React from 'react';
import { Clock, Plus, Save, Trash2 } from 'lucide-react';
import { useDeliverySlaConfig } from '../slaConfigHooks';

// DeliverySlaConfigCardProps 描述发货 SLA 配置卡的挂载开关。
interface DeliverySlaConfigCardProps {
  /** visible 表示当前用户是否为管理员，非管理员不读取也不渲染配置卡。 */
  visible: boolean;
}

/** DeliverySlaConfigCard 渲染订单页顶部的发货 SLA 配置卡：默认分钟数与账号覆盖。 */
export const DeliverySlaConfigCard: React.FC<DeliverySlaConfigCardProps> = ({ visible }) => {
  // sla 是发货 SLA 配置的读取、草稿编辑与保存状态。
  const sla = useDeliverySlaConfig(visible);
  // { 解构出发货 SLA 配置卡需要的状态和操作。
  const { config, invalidJson, accounts, loading, saving, message, messageKind, setDefaultMinutes, upsertAccountOverride, removeAccountOverride, save } = sla;
  // overrideRows 是按插入顺序展开的账号覆盖行。
  const overrideRows = Object.entries(config.per_account);
  // usedCookieIds 是已被覆盖行占用的账号标识集合，防止重复添加。
  const usedCookieIds = new Set(overrideRows.map(/* entry 是账号覆盖键值对。 */ entry => entry[0]));
  // availableAccounts 是仍可添加为覆盖行的账号选项。
  const availableAccounts = accounts.filter(/* account 是待筛选的账号选项。 */ account => !usedCookieIds.has(account.cookie_id));
  if (!visible) return null;

  // addOverrideRow 为第一个尚未覆盖的账号新增一行覆盖配置。
  const addOverrideRow = (): void => {
    // next 是待新增覆盖的账号选项，无可用账号时不新增。
    const next = availableAccounts[0];
    if (!next) return;
    upsertAccountOverride(next.cookie_id, config.default_minutes);
  };

  return (
    <div className="ios-card rounded-xl p-6 bg-white space-y-5">
      <div className="flex items-start gap-2">
        <div className="p-1.5 rounded-lg bg-blue-500 text-white mt-0.5">
          <Clock className="w-4 h-4" />
        </div>
        <div>
          <h4 className="text-sm font-bold text-gray-800">发货 SLA</h4>
          <p className="text-xs text-gray-500 mt-0.5">付款后应在分钟数内发货；0 表示关闭。账号覆盖优先于默认值。</p>
        </div>
      </div>

      {invalidJson && (
        <p className="text-xs font-medium text-red-600">配置 JSON 非法，已回退默认值；修改任一项后保存将覆盖为合法 JSON。</p>
      )}

      <div className="space-y-2">
        <label className="block text-sm font-bold text-gray-800" htmlFor="delivery-sla-default-minutes">默认分钟数</label>
        <input
          id="delivery-sla-default-minutes"
          type="number"
          min={0}
          value={config.default_minutes}
          disabled={loading}
          onChange={/* defaultMinutesChange 同步默认分钟数草稿。 */ event => setDefaultMinutes(Number(event.target.value))}
          className="w-48 ios-input px-4 py-3 rounded-xl"
        />
        <p className="text-xs text-gray-500">单位：分钟；0=关闭。剩余不足 20% 时倒计时变黄，超时变红。</p>
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h5 className="text-sm font-bold text-gray-800">账号覆盖</h5>
          <button
            type="button"
            onClick={addOverrideRow}
            disabled={availableAccounts.length === 0}
            className="inline-flex items-center gap-1 rounded-lg bg-gray-100 px-3 py-2 text-xs font-bold text-gray-700 transition-colors hover:bg-gray-200 disabled:opacity-50"
          >
            <Plus className="w-3.5 h-3.5" />
            添加账号覆盖
          </button>
        </div>
        {overrideRows.length === 0 && (
          <p className="text-xs text-gray-500">暂无账号覆盖，全部账号使用默认分钟数。</p>
        )}
        {overrideRows.map(/* entry 是当前编辑中的账号覆盖行。 */ ([cookieId, minutes]) => (
          <div key={cookieId} className="flex items-center gap-3 rounded-xl border border-gray-200 bg-gray-50/70 p-3">
            <select
              aria-label={`账号覆盖 ${cookieId}`}
              value={cookieId}
              disabled={loading || saving}
              onChange={/* accountChange 将当前覆盖行迁移到新账号。 */ event => {
                // nextCookieId 是迁移后的账号标识，空值忽略。
                const nextCookieId = event.target.value;
                if (!nextCookieId) return;
                removeAccountOverride(cookieId);
                upsertAccountOverride(nextCookieId, minutes);
              }}
              className="ios-input px-3 py-2 rounded-xl text-sm min-w-0 flex-1"
            >
              <option value={cookieId}>{accounts.find(/* account 是用于展示名称的账号选项。 */ account => account.cookie_id === cookieId)?.label || cookieId}</option>
              {availableAccounts.map(/* account 是仍可迁移的账号选项。 */ account => (
                <option key={account.cookie_id} value={account.cookie_id}>{account.label}</option>
              ))}
            </select>
            <input
              type="number"
              min={0}
              aria-label={`覆盖分钟数 ${cookieId}`}
              value={minutes}
              disabled={loading || saving}
              onChange={/* minutesChange 同步当前账号的覆盖分钟数草稿。 */ event => upsertAccountOverride(cookieId, Number(event.target.value))}
              className="w-28 ios-input px-3 py-2 rounded-xl text-sm"
            />
            <button
              type="button"
              onClick={/* removeRow 删除当前账号覆盖行。 */ () => removeAccountOverride(cookieId)}
              aria-label={`删除账号覆盖 ${cookieId}`}
              className="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-100"
            >
              <Trash2 className="w-4 h-4" />
            </button>
          </div>
        ))}
      </div>

      {message && (
        <p className={`text-xs font-medium ${messageKind === 'success' ? 'text-green-600' : 'text-red-600'}`}>{message}</p>
      )}

      <button
        type="button"
        onClick={/* saveClick 提交当前 SLA 配置草稿。 */ () => void save()}
        disabled={loading || saving}
        className="inline-flex items-center gap-2 rounded-xl bg-blue-600 px-5 py-2.5 text-sm font-bold text-white transition-colors hover:bg-blue-700 disabled:opacity-50"
      >
        <Save className="w-4 h-4" />
        保存 SLA 配置
      </button>
    </div>
  );
};
