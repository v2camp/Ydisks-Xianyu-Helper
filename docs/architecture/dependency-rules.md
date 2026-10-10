# 架构依赖与边界规则

## 1. 目的

本文定义重构期间和目标状态下的依赖方向。过渡期允许保留已有直接依赖，
但禁止新增同类耦合；每个正式阶段必须减少而不是扩大例外。阶段编号、状态和交付规则只由
refactoring-master-plan.md 定义，本文不声明当前阶段或完成状态。

## 2. 通用规则

- 接口由能力消费者定义，不在实现包中建立包含所有方法的万能接口；
- 上层依赖抽象，下层实现抽象；
- 不为消除 import 而创建没有业务语义的中转包；
- 不允许全局 service locator；
- 必需依赖在构造阶段提供并验证，不使用运行时 setter 回填；
- package 不得因测试方便而导出生产不需要的内部状态；
- 不允许通过 `any`、动态 map 或反射绕过清晰依赖。

## 3. Go 目标边界

### 3.1 `cmd`

允许：

- 读取配置和环境；
- 构造日志、数据库和应用；
- 处理系统信号；
- 调用应用 Start/Run/Stop。

禁止新增：

- 业务规则；
- SQL；
- HTTP handler；
- 平台协议解析；
- 账号状态机。

### 3.2 `internal/server`

允许：

- 路由和 middleware；
- 鉴权上下文；
- 请求解析、验证和 DTO；
- 调用应用服务；
- HTTP/WebSocket transport；
- SPA 静态资源。

目标状态禁止：

- 直接导入 `internal/db`；
- 直接导入 `internal/xianyu` 或 `internal/browser`；
- 调用 `BeginTx`；
- 保存业务 worker、锁、cancel map 或平台会话状态；
- 直接实现订单同步、商品发布、凭证续期或自动化动作。

过渡期已有依赖可以在对应正式阶段完成前保留，但不得新增新的直接调用点。

### 3.3 应用服务

允许：

- 编排一个完整用户用例；
- 所有权校验；
- 事务边界；
- 调用领域服务和消费者定义的 port；
- 将基础设施错误转换为应用错误。

禁止：

- 依赖 `http.Request`、`http.ResponseWriter` 或 chi；
- 依赖 React/前端字段别名；
- 拼装 SQL；
- 直接读取环境变量；
- 返回包含明文秘密的通用模型。

### 3.4 `internal/db`

允许：

- SQL、迁移、方言差异；
- repository 实现；
- 加密存储；
- 事务实现；
- 持久化查询模型。

禁止：

- 导入 server、应用服务、engine 或 automation；
- 依赖 HTTP DTO；
- 调用 MTOP、WebSocket 或 browser；
- 决定用户可见错误消息；
- 将敏感持久化模型直接暴露给 HTTP。

### 3.5 `internal/xianyu` 与 `internal/browser`

允许：

- 平台协议、传输和浏览器实现；
- 返回平台级结果和错误分类；
- 实现应用层或领域层定义的最小接口。

禁止：

- 导入 server；
- 直接写业务数据库；
- 决定自动化规则或 HTTP 状态；
- 反向启动 account manager；
- 绕过冻结滑块规范。

### 3.6 `internal/engine` 与 `internal/automation`

- Engine 负责单账号消息运行时，不负责 HTTP；
- Automation 负责规则运行和动作语义，不负责 HTTP DTO；
- 两者不得依赖 Server；
- 新依赖必须通过最小接口注入；
- 并发状态必须由明确组件拥有，禁止共享无边界的可变结构；
- 外部动作必须保留幂等、checkpoint 和结果不确定语义。

### 3.7 `internal/mcp`（对外开放 MCP 传输层）

`internal/mcp` 是与 `internal/server` 并列的第二传输层，对 Harness 提供 Streamable HTTP 协议服务。

允许：

- 依赖标准库与 `github.com/mark3labs/mcp-go`；
- 依赖 `internal/application/*` 的应用模型；
- 依赖 `internal/capability` 的共享用例契约（`ports.go` 的领域端口）与危险等级；
- 在包内定义只有本传输层使用的协议与装配端口：鉴权身份、调用审计、自身配置、
  只读资源与提示所需的消费者端口。业务领域用例契约一律取自 `internal/capability`，
  不在本包复数定义。

禁止：

- 导入 `internal/server`、`internal/db`、`internal/xianyu`、`internal/browser`；
- 导入 `internal/automation`、`internal/engine`、`internal/adapter`、`internal/composition`；
- 直接拼装 SQL、读取平台凭证、决定自动化规则或浏览器行为；
- 自行声明工具的危险等级，或自行推导放行结论；
- 使用万能服务容器或服务定位器，端口必须由组合层投影实现。

`internal/server` 只保留 `/mcp` 的通用挂载缝：不得导入 `mcp-go`，也不得出现 JSON-RPC 协议语义。
`/mcp` 是非业务协议端点，不进 OpenAPI 登记；管理员管理接口固定在 `/api/v1/mcp/*` 并登记进 `api/openapi.yaml`。
`XIANYU_MCP_TOKEN` 是部署者通过进程环境注入的引导令牌，应用只从环境读取，不落库、不写日志。
00057 迁移建立 `mcp_tokens`（只存令牌哈希）与 `mcp_call_audit`（键级脱敏审计）两表，两种方言结构一致。

### 3.8 `internal/capability`（能力目录与访问策略）

`internal/capability` 定义平台能力的统一目录与访问策略：能力清单、危险等级、作用域与各调用方
所需的确认方式。它是权限判断的唯一实现点，三个消费方（`internal/mcp` 承载的外部 Harness、
`internal/qqbot` 承载的运营 Agent、`internal/agent` 承载的客服 Agent）都必须经它求值。
它还承载被多个消费方共用的用例契约（`ports.go`），这是同名用例只有一个签名来源的保证。

允许：

- 依赖标准库；
- 依赖 `internal/application/*` 的应用模型。

禁止：

- 导入 `internal/mcp`、`internal/server` 等传输层；
- 导入 `internal/db`、`internal/xianyu`、`internal/browser`、`internal/automation`、`internal/engine`；
- 导入 `internal/adapter`、`internal/composition`；
- 在本包内执行能力或直接触达业务数据。

边界说明：本包只回答「这次调用是否允许、需要何种确认」，不执行能力。
执行由消费方在取得放行决策后调用应用层用例完成；消费方不得自行推导权限结论，
也不得因本包返回拒绝而降级为「直接调用用例」。

MCP 工具声明的危险等级必须取自本包的标注表（`tool_risk.go`），由传输层注册框架按名
投影填入，传输层不得自行标注；未登记等级的工具不注册，不得按默认等级放行。
标注表与决策目录 `PlatformCatalog` 是两张表：前者描述「动作危险到什么程度」，覆盖传输层
实际暴露的全部工具；后者决定「谁现在可以调用」，只登记已接入消费方的能力。

### 3.9 `internal/agent`（客服 Agent 运行时）

`internal/agent` 承载面向单账号的客服 Agent 运行时：回合预算、步骤上限与转人工逃生阀。
它是能力内核的消费方，也是模型生成接缝的实现方，不是传输层。

允许：

- 依赖标准库与模型 SDK（`github.com/sashabaranov/go-openai`）；
- 依赖 `internal/netguard` 构造受控 HTTP 客户端；
- 依赖 `internal/application/*` 的应用模型；
- 依赖 `internal/capability` 取得放行决策与用例端口契约；
- 依赖 `internal/engine` 的模型生成接缝（`AIGenerator` / `GenerateRequest`）。

禁止：

- 导入 `internal/db`、`internal/xianyu`、`internal/browser`、`internal/automation`；
- 导入 `internal/mcp`、`internal/qqbot`、`internal/server` 等传输层；
- 导入 `internal/adapter`、`internal/composition`；
- 自行推导权限结论，或在本包返回拒绝后降级为直接调用用例。

边界说明：

- Agent 侧作用域被强制锁定为当前账号，无法跨账号；每次工具调用前都要经
  `capability.ResolveAccountScope` 断言，禁止信任模型给出的账号参数。
- 只实现 engine 的模型生成接缝，不得调用 engine 的消息收发、连接管理与凭证链路。
  依赖方向由此固定为 `agent -> engine`，engine 不反向依赖 agent，注入由组合层完成。
- `internal/agent/mcpclient` 承载对外第三方 MCP 客户端（如找书 MCP），与主循环同属本边界，
  但必须独立故障域与超时；第三方 MCP 返回不得绕过业务护栏直接落到账号动作。

### 3.10 `internal/application/agentadmin`（客服 Agent 配置用例）

`internal/application/agentadmin` 只回答两个问题：这个账号跑不跑客服 Agent、跑在哪一档。
平台级定义（有哪些能力、各属哪一档）不在这里，它是 `internal/capability` 的代码常量，
用户不可增删；本包只负责在租户默认与账号覆盖之间选出最终生效值。

配置分层的落点：

| 层 | 载体 | 本包角色 |
|---|---|---|
| 平台级 | `internal/capability` 的代码常量 | 只读消费 |
| 租户级 | `user_settings` 的 `agent.support.enabled` / `agent.support.preset` | 读写 |
| 账号级 | `ai_reply_settings` 的 `agent_enabled` / `agent_preset`，NULL 表示继承 | 读写 |

允许：

- 依赖标准库；
- 依赖 `internal/capability` 的档位常量与合法性判定。

禁止：

- 导入 `internal/db`、`internal/server`、`net/http`（应用层通用规则）；
- 返回含模型凭证的持久化模型：账号级配置与 `api_key` 同表，传输模型必须单独定义，
  禁止复用 `AIReplySettings`（AGENTS.md §1.7）；
- 自行列举合法档位：判定单点是 `capability.Preset.Valid()`。

边界说明：

- 读写两条路径的失败策略刻意不同。库中遗留的非法档位在读取时回落只读档，让 Agent 以
  最保守档位继续服务；写入时直接拒绝，避免「配了什么」与「生效什么」长期不一致。
- 账号归属校验内嵌在用例内。账号不存在与不属于当前租户共用同一个错误，避免调用方接口
  退化成账号枚举器；调用方不得把归属失败降级为「未配置」继续执行。
- 日预算不在本期配置项内：它的执行语义（按账号按日计数与扣减）尚未实现，先暴露一个不
  生效的开关与「配置项必须生效」的约定冲突。

## 4. 数据与秘密边界

- AccountSummary 不包含 Cookie、Token、密码或加密 metadata；
- AccountCredential 只能进入平台调用和凭证更新流程；
- AccountLoginSecret 仅作为历史密码登录模型或明确授权的续期流程输入；当前产品密码登录入口和自动恢复路径均已关闭，生产账号恢复不得读取或调用浏览器密码登录；
- 所有权查询只能返回存在性或非敏感 ID；
- HTTP DTO 不得引用敏感模型；
- 日志、通知和错误响应不得包含秘密；
- 测试失败输出不得打印完整秘密；
- 锁住账号凭证期间不得执行未明确允许的慢外部 I/O。

## 5. API 边界

- 新 API 使用 `/api/v1`；
- 新请求和响应必须使用具名 DTO；
- 新错误使用统一错误 envelope；
- 禁止新增 HTTP 200 + `success:false`；
- 禁止新增 `value/cookie`、`remark/note` 一类双重字段；
- 兼容字段只能在边界 adapter 中归一；
- 删除兼容层必须有调用方迁移和契约测试证据。
- `/api/v1/**` 与 `/health` 的唯一 HTTP 契约源是 `api/openapi.yaml`；新增 operation 必须同步更新规范、生成
  TypeScript schema 和真实 handler 响应校验，禁止恢复手写 `transport.ts` 或 DTO 名单门禁。
- `/mcp` 是非业务协议端点，不进 OpenAPI；MCP 管理接口固定在 `/api/v1/mcp/*` 并登记进规范。
- MCP 令牌明文只在生成/轮换响应出现一次，状态与审计接口永不回显明文。

## 6. React 边界

目标结构为 `app -> features -> shared`：

- `app` 负责 shell、路由和顶层认证；
- feature 负责自己的页面、Hook、组件、API adapter 和类型；
- shared 只包含真正跨 feature 的请求客户端、UI、Hook 和工具；
- feature 不得导入另一个 feature 的内部文件；
- shared 不得反向导入 feature；
- 组件不得直接调用 `fetch` 或 `axios`；
- 通用 HTTP client 不包含订单、账号等领域归一逻辑；
- 生成 API 类型只读，UI model 由 feature adapter 创建；
- 每个 feature 只有一个 `api.ts` 承担传输契约读取与 UI model 转换，其余文件只能依赖它；
- 生成 schema 只能由 shared 契约层导入，feature、组件和 Hook 不得直接读取；
- 原始 `get/post/put/del/postForm` 只能留在旧客户端兼容实现，feature 不得导入或调用；
- 禁止通过大型 barrel 文件隐藏实际依赖。

## 7. 事务与生命周期边界

- 跨 repository 原子操作由应用服务通过 Unit of Work 执行；
- handler 和 React 层不得控制数据库事务；
- goroutine 必须有明确 owner、Context 和 Wait；
- channel 只能由约定的发送方关闭；
- Start 前不隐式运行后台任务；
- Stop/Close 必须幂等；
- 不得持锁等待无法控制的网络、浏览器或用户操作；
- 凭证锁、自动化卡密锁和 worker 锁的顺序必须被注释和测试保护。

## 8. 自动门禁路线

门禁应按目标边界逐步启用：

1. 当前正式阶段开始前先启用该阶段的 fail-closed AST、依赖或行为门禁；当前阶段违规必须立即失败。
2. 已完成阶段的门禁永久保留；后续阶段专有门禁只在该阶段成为当前阶段时启用。
3. 阶段目标边界禁止使用白名单、baseline、忽略目录或降级告警豁免；阶段内失败只能通过完成迁移消除。
4. 最终使用 `go list`、AST 与前端依赖规则检查完整依赖图，不能只依赖少量名称匹配。
5. 仅生成物和冻结文件可有精确范围排除；它们不是架构违规的通用例外，也不能覆盖生产源码。
6. 禁止为了让门禁通过而把依赖藏进反射、动态 import、service locator 或无语义中转层。
