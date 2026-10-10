// Package capability 定义平台能力的统一目录与访问策略：有哪些能力、危险到什么程度、
// 谁能调用、需要何种确认。
//
// 本包是能力策略的唯一实现点。三个消费方——外部 Harness、运营 Agent、客服 Agent——
// 都必须经本包求值后再触达应用层用例；禁止在传输层或 Agent 层各自复制一份权限判断，
// 否则同一能力在不同入口的放行口径必然漂移。
//
// 依赖方向：本包只依赖标准库与 internal/application/* 的应用模型。
// 本包不依赖任何传输层（internal/mcp、internal/server）、实现层（internal/db、
// internal/xianyu、internal/browser、internal/automation、internal/engine）
// 或装配根（internal/adapter、internal/composition）。
//
// 本包不执行能力，只回答「这次调用是否允许、需要何种确认」。
// 实际执行由消费方在拿到允许决策后调用应用层用例完成。
package capability
