// ai_generator.go 定义「调用模型」这一步的可替换接缝。
//
// 客服 Agent Loop 通过实现 AIGenerator 替换生成过程（例如带工具回合的多步生成），
// 而价格边界校验、内部报价标记提取、意图判定与对话落库仍由 AIReplierImpl 统一执行，
// 不随生成器实现变化——替换的是「怎么问模型」，不是「怎么校验模型说了什么」。

package engine

import "context"

// AIGeneratorFactory 按账号构造模型生成接缝。
// 返回 nil 表示该账号不使用自定义生成器（例如客服 Agent 未启用），沿用默认单次问答实现。
// 工厂由组合层提供：engine 不反向依赖任何生成器实现，避免形成依赖环。
type AIGeneratorFactory func(cookieID string) AIGenerator

// AIGenerator 抽象一次模型补全调用。
//
// 实现只负责产出候选文本：不得改写调用方传入的请求，也不得自行裁剪或改写回复正文。
// 价格守卫与内部报价标记的提取由调用方完成，因此实现返回的文本必须保留模型原始输出
// （包括 [[AUTO_PRICE:...]] 标记），否则会让 AIReplierImpl 的价格校验失去依据。
type AIGenerator interface {
	// Generate 执行一次模型补全并返回首个候选文本。
	// ctx 是调用方上下文；req 是本次补全的全部输入。
	// 返回空字符串且 error 为 nil 表示模型没有产出可用内容，调用方按降级处理。
	Generate(ctx context.Context, req GenerateRequest) (string, error)
}

// GenerateRequest 是一次模型补全所需的全部输入。
//
// 密钥、地址与模型名由调用方从账号级与全局设置解析后注入，生成器不自行读库，
// 以保证生成器实现可以被替换或包装而不改变配置读取口径。
type GenerateRequest struct {
	// APIKey 是 OpenAI 兼容接口的密钥，属敏感值，禁止写入日志、审计或测试失败输出。
	APIKey string
	// BaseURL 是接口基地址；空字符串表示沿用 go-openai 的默认地址。
	BaseURL string
	// Model 是模型名；空字符串表示沿用实现内置的默认模型。
	Model string
	// Temperature 是采样温度，由调用方明确给出，便于评测固定为 0。
	Temperature float32
	// System 是 system 提示词，已由调用方拼装完业务护栏与知识上下文。
	System string
	// History 是既有对话历史，按时间正序；长度裁剪由生成器实现负责。
	History []ConversationTurn
	// Current 是本次待回复的买家文本。
	Current string
}

// ConversationTurn 是一轮既存对话。
type ConversationTurn struct {
	// Role 为 "assistant" 时按助手消息处理，其余取值一律按买家消息处理。
	Role string
	// Content 是该轮文本。
	Content string
}
