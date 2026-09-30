# P1 实施计划：发货 SLA 倒计时 + 话术重复风控提示

- 日期：2026-09-30
- 分支：feat/sla-and-copy-risk
- 前置：发货护栏 P0 已合入（c72ef96）

## 契约（前后端共同约定，先写死）

`OrderDTO` 新增可选字段（api/openapi.yaml 先改，再 `npm --prefix frontend run api:generate`）：

| 字段 | 类型 | 含义 |
|---|---|---|
| `paid_at` | string|null | 付款时间 |
| `shipped_at` | string|null | 发货时间 |
| `sla_minutes` | integer | 生效的发货 SLA 分钟数，0=未启用 |
| `sla_deadline` | string|null | 付款时间+SLA 的截止时刻，未启用为空 |

设置键 `delivery_sla_config`（系统设置，普通键）JSON：
`{"default_minutes":0,"per_account":{"<cookie_id>":120}}`
- `default_minutes` 0=关闭；per_account 覆盖默认；非法 JSON 拒绝落库，空串=清空。
- 计算归属：handler 组装 OrderDTO 时读取设置并计算 `sla_minutes`/`sla_deadline`（前端只展示，不读设置）。

## 后端（A）

1. openapi.yaml：OrderDTO 四字段；重生成前端类型；契约测试补齐。
2. order_handlers DTO 映射补 paid_at/shipped_at；按 cookie_id 取 SLA 计算两字段。
3. settings/service.go：`delivery_sla_config` 校验（非负整数、合法 JSON、键名长度）。
4. 超时提醒看门狗（internal/automation，参考 watchdog.go）：
   - 周期扫描 `order_status` 待发货且 paid_at 非空、shipped_at 空的订单；
   - elapsed ≥ 80% SLA 且 < 100% → 提醒级通知；≥ 100% → 升级级通知；
   - 每订单每级别只发一次（进程内去重 + 通知历史近 48h 查重，双保险；重启允许极少重复，文档化）；
   - 通知走现有账号事件通知（NotifyAccountEvent），event `delivery_sla_warning` / `delivery_sla_overdue`，标题含订单号与剩余/超时分钟；
   - 无锁网络/睡眠；可关停；SLA=0 或缺 paid_at 跳过。
5. 测试：DTO 映射、SLA 计算（默认/覆盖/未启用）、看门狗阈值边界（79%/80%/100%）、去重、关停、通知内容不含敏感字段。automation 核心链路新代码覆盖 100%。

## 前端（B+C）

B 订单面板（features/orders）：
1. 订单表格「发货倒计时」列：sla_deadline 存在时显示剩余时间；<20% 剩余黄色、已超时红色加「已超时」标；未启用显示「—」。付款/发货时间在详情或悬停提示可见。
2. 「发货 SLA」配置卡（页面顶部，仅管理员）：默认分钟输入 + 账号覆盖列表（账号来自共享 HTTP `/api/v1/accounts`，经本 feature 适配器），保存写 `delivery_sla_config`（settings API，经共享客户端）。
3. 倒计时每 30s 本地刷新；文案单位分钟/小时。

C 话术重复风控提示（features/delivery-templates）：
1. 模板消息编辑区：当某条消息文本与其它模板任一消息完全一致（trim 后）→ 该行黄色警示「与其它模板文案完全一致，高频重复发送易触发营销风控」。
2. 编辑器顶部固定提示条：说明固定话术高频重复的风险（社区经验），建议同义微调、避免逐字群发。
3. 纯前端；不改 API。

## 验证

- go test ./internal/automation ./internal/application/settings ./internal/server（相关）；核心链路覆盖率不降
- commentlint、前端 typecheck/test/comments:check
- webui-e2e（页面渲染改动）
- 契约：npm --prefix frontend run api:check
