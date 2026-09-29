# 发货护栏 P0 实施计划（词库门禁 + 链接健康检查）

- 日期：2026-09-30
- 依据：docs/research/2026-09-29-xianyu-policy-reference.md §5.5
- 分支：feat/delivery-guardrails

## 优先级总览（本轮只做 P0）

| 优先级 | 项 | 理由 |
|---|---|---|
| P0 | 发货内容违禁词/外链门禁 | 私聊属发布信息，自动发货内容即证据；命中引导站外/联系方式是硬红线 |
| P0 | 链接健康检查 | 争议规范 109 条：无法使用→全额退款；失效链接先拦后发 |
| P1 | SLA 发货倒计时面板 | 「描述不符全额退」72h/48h 超时自动同意退款 |
| P1 | 话术重复风控提示 | 固定重复话术易判营销 |
| P2 | 合规声明模板/节流/多号提示 | 降险增益较小或依赖外部数据 |

## P0-1 发货内容门禁

- 新模块 `internal/automation/delivery_guard.go`：
  - `ScanDeliveryContent(text) []ContentHit`；命中即拒发。
  - 规则：①联系方式/引流（微信|加微|加V|维信|薇信|QQ|手机号 11 位|二维码|扫码|站外…）②非网盘域名的 http(s) URL ③内置违禁词（侵权/色情/政治等引流高危词）④额外违禁词（设置键）。
  - 网盘域名放行（pan.baidu.com、pan.quark.cn、云盘/迅雷等常见域），这是交付物本身。
- 设置键 `delivery_content_guard`：JSON `{enabled:bool, extra_block_words:string}`，默认 enabled=true；空缺=启用内置规则。
- 挂载点：`action_executor` 与 `card_delivery` 所有出站文本（send_text/send_card/send_template/安抚话术）发送前扫描；命中→不发送，运行置 needs_review 并发「需要人工处理」通知（复用 notifyRunNeedsReview 路径）。
- 门禁失败不得自动重试（区别于网络失败）。

## P0-2 链接健康检查

- 同模块 `CheckDeliveryLinks(ctx, text) []LinkCheckResult`：提取 http(s) URL，经 netguard 出站策略发起 HEAD（5s 超时，失败降级 GET）。
- 判定：DNS/连接/超时失败 → 拦截转 needs_review；404/410 → 拦截；2xx/3xx → 通过；其它 4xx/5xx → 仅告警放行（网盘反爬常 403，不可一概拦截）。
- 设置键并入 `delivery_content_guard.link_check: bool`，默认 true。
- 检查在发送前、无锁环境执行；单次运行最多检查 5 个 URL，防滥用。

## 文件所有权（并行）

- G1：`internal/automation/**`、`internal/application/settings/service.go|service_test.go`（键校验）
- G2：`frontend/app/features/settings/**`（安全阀门区新增门禁开关/额外违禁词/链接检查开关）

## 验证

- go test ./internal/automation ./internal/application/settings（automation 核心链路，新代码语句覆盖 100%）
- commentlint、前端 typecheck/test/comments:check
- webui-e2e（前端有页面渲染改动）
