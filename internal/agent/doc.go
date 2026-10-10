// doc.go 客服 Agent 运行时。
//
// 本包实现面向单个账号的客服 Agent：在既有单次问答链路上叠加「工具回合」，
// 让模型可以调用平台只读能力（订单查询、商品查询）后再作答。
//
// 三条硬约束：
//
//  1. 只替换生成、不替换校验。本包通过实现 internal/engine 的 AIGenerator 接入，
//     生成器之上仍旧由 engine 的价格守卫、意图判定与对话落库负责，
//     Agent 无权重写或绕过这些校验。
//  2. 作用域强制锁定当前账号。每次工具调用都先经 capability.Evaluate 求值，再经
//     capability.ResolveAccountScope 断言账号作用域；模型给出的账号参数不被信任。
//  3. 失败即回落。回合预算耗尽、模型异常或工具链路故障时回落到单次问答生成器，
//     保证买家始终能收到一条回复，而不是静默失败。
//
// 依赖边界见 AGENTS.md §1.1 与 docs/architecture/dependency-rules.md §3.9：
// 本包禁止依赖 db、平台、浏览器、自动化、mcp、qqbot、server 与 adapter。
package agent
