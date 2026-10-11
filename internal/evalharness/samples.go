// samples.go 加载缺陷样本库并推导可机检的判定期望。
//
// 样本库由 cmd/evalexport 产出（data/evalexport/samples.jsonl，已被 .gitignore 忽略）。
// 判定期望的来源有两类：样本内嵌的人工标注，以及本文件的高置信自动推导；
// 自动推导的结果会在报告中显式标注，提醒人工复核。
package evalharness

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// deliveryChannelPattern 匹配提示中的发货渠道声明，同时兼容两种历史格式：
// 旧格式「该商品发货方式（按该商品卡密内容确定，可直接回答）：渠道」
// 与新格式「【该商品确定发货方式】渠道」。
// 分组含义：1 是旧格式的依据说明，2 是旧格式的渠道正文，3 是新格式的渠道正文。
// 兼容两种格式是可比性的前提：若新格式推导不出期望而旧格式能推导，
// 提示词改动会同时改变判定口径，修复前后的评测结果将无法对照。
var deliveryChannelPattern = regexp.MustCompile(`该商品发货方式（([^）]*)）[:：]\s*([^。；\n]+)|【该商品确定发货方式】\s*([^。；\n]+)`)

// AITurn 是样本中的一条 AI 对话记录。
type AITurn struct {
	// Role 是角色：user 买家，assistant AI 回复。
	Role string `json:"role"`
	// Content 是消息文本。
	Content string `json:"content"`
	// Intent 是意图标签。
	Intent string `json:"intent"`
}

// Message 是样本中的一条聊天记录。
type Message struct {
	// Direction 是消息方向：incoming 买家，outgoing 卖家。
	Direction string `json:"direction"`
	// MessageType 是消息类型：text、image、system 等。
	MessageType string `json:"message_type"`
	// Content 是消息文本或媒体地址。
	Content string `json:"content"`
	// SentAt 是发送时间（Unix 毫秒）。
	SentAt int64 `json:"sent_at"`
}

// ConversationTurn 是提示历史中的一轮对话。
type ConversationTurn struct {
	// Role 是角色：user 买家，assistant 卖家。
	Role string `json:"Role"`
	// Content 是该轮文本。
	Content string `json:"Content"`
}

// Snapshot 是样本中按生产链路重建的提示快照（只取评测所需字段）。
type Snapshot struct {
	// Model 是当时生效的模型标识。
	Model string `json:"Model"`
	// SystemPrompt 是重建出的完整 system 提示。
	SystemPrompt string `json:"SystemPrompt"`
	// KnowledgeContext 是当时注入的知识上下文。
	KnowledgeContext string `json:"KnowledgeContext"`
	// History 是拼装时可见的历史轮次。
	History []ConversationTurn `json:"History"`
	// MessageText 是触发本次回复的买家消息。
	MessageText string `json:"MessageText"`
	// Takeover 表示该消息在生产判定链下是否允许 AI 接管。
	Takeover bool `json:"Takeover"`
	// Intent 是意图标签。
	Intent string `json:"Intent"`
	// ItemPrice 是商品标价。
	ItemPrice float64 `json:"ItemPrice"`
	// MinimumPrice 是最低可成交价。
	MinimumPrice float64 `json:"MinimumPrice"`
}

// Expectation 是一条样本的判定期望。
type Expectation struct {
	// MustContainAny 是回复必须至少包含其中之一的片段；为空表示不检查。
	MustContainAny []string `json:"must_contain_any,omitempty"`
	// MustNotContain 是回复不得出现的片段，任一出现即判失败；为空表示不检查。
	MustNotContain []string `json:"must_not_contain,omitempty"`
	// PromptMustContain 是系统提示必须包含的片段（结构层断言，不调用模型）。
	PromptMustContain []string `json:"prompt_must_contain,omitempty"`
	// Derived 表示该期望由工具自动推导，人工复核确认后应清除该标记。
	Derived bool `json:"derived,omitempty"`
	// Note 是期望来源说明；非空时会作为额外判定要求注入判定模型的提示。
	Note string `json:"note,omitempty"`
	// MustNotTakeover 显式声明该消息是否应被 AI 接管。
	// 声明后走决策层判定：只校验接管结论，不调用任何模型，成本为零且完全确定。
	MustNotTakeover *bool `json:"must_not_takeover,omitempty"`
}

// Sample 是缺陷样本库中的一条样本。
type Sample struct {
	// Account 是哈希后的账号标识。
	Account string `json:"account"`
	// Chat 是哈希后的会话标识。
	Chat string `json:"chat"`
	// Item 是哈希后的商品标识。
	Item string `json:"item"`
	// Verdicts 是事故判定标签集合。
	Verdicts []string `json:"verdicts"`
	// Messages 是该会话的完整聊天记录。
	Messages []Message `json:"messages"`
	// AITurns 是该会话的 AI 对话记录；自检模式用它取历史真实回复。
	AITurns []AITurn `json:"ai_turns"`
	// Snapshot 是重建的提示快照；缺失时该样本无法回放。
	Snapshot *Snapshot `json:"snapshot"`
	// Expectation 是人工标注的判定期望；为空时由工具自动推导。
	Expectation *Expectation `json:"expectation"`
	// Source 是样本来源：production 表示生产事故导出，synthetic 表示按场景合成。
	Source string `json:"source,omitempty"`
	// ScenarioID 是合成场景的稳定标识；生产样本为空。
	ScenarioID string `json:"scenario_id,omitempty"`
}

// LoadSamples 从 JSONL 文件读取样本库。
// 空行会被跳过；单行解析失败会带上行号返回，便于定位样本库损坏位置。
func LoadSamples(path string) ([]Sample, error) {
	// f、err 是样本库文件句柄与打开失败原因。
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开样本库失败: %w", err)
	}
	defer f.Close()

	// samples 是解析出的样本列表。
	samples := make([]Sample, 0, 64)
	// scanner 按行读取，缓冲区放大以容纳单条含完整提示的长记录。
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	// lineNo 是当前行号（从 1 开始）。
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		// line 是当前行的原始内容。
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// sample 是当前行的解析结果。
		var sample Sample
		// unmarshalErr 是当前行的反序列化失败原因。
		if unmarshalErr := json.Unmarshal([]byte(line), &sample); unmarshalErr != nil {
			return nil, fmt.Errorf("解析样本库第 %d 行失败: %w", lineNo, unmarshalErr)
		}
		samples = append(samples, sample)
	}
	// scanErr 是逐行读取过程中的累计失败原因。
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("读取样本库失败: %w", scanErr)
	}
	return samples, nil
}

// Replayable 判断该样本是否具备回放条件：需要买家消息与重建的 system 提示。
func (s Sample) Replayable() bool {
	return s.Snapshot != nil &&
		strings.TrimSpace(s.Snapshot.MessageText) != "" &&
		strings.TrimSpace(s.Snapshot.SystemPrompt) != ""
}

// ResolveExpectation 返回该样本的判定期望：优先用人工标注，缺失时自动推导。
// 返回的期望带 Derived 标记时，表示它来自自动推导、尚待人工复核。
func (s Sample) ResolveExpectation() *Expectation {
	if s.Expectation != nil {
		return s.Expectation
	}
	return deriveExpectation(s)
}

// deriveExpectation 从样本快照推导高置信的判定期望。
// 仅做一条推导：提示已声明「确定发货渠道」时，要求回复至少给出其中一个渠道。
// 其余场景返回 nil，交由 LLM judge 判定，避免用不可靠的规则制造假阳性。
func deriveExpectation(s Sample) *Expectation {
	if s.Snapshot == nil {
		return nil
	}
	// match 是提示中发货渠道声明的匹配结果。
	match := deliveryChannelPattern.FindStringSubmatch(s.Snapshot.SystemPrompt)
	if len(match) < 4 {
		return nil
	}
	// basis 是旧格式的依据说明；为空表示命中的是新格式。
	basis := strings.TrimSpace(match[1])
	// raw 是匹配到的渠道正文。
	raw := strings.TrimSpace(match[2])
	if raw == "" {
		raw = strings.TrimSpace(match[3])
	}
	// 旧格式只有依据含「确定」时才是权威结论；标注「仅参考」时不推导强期望。
	if basis != "" && !strings.Contains(basis, "确定") {
		return nil
	}
	// channels 是声明中的渠道名列表。
	channels := splitChannels(raw)
	if len(channels) == 0 {
		return nil
	}
	return &Expectation{
		MustContainAny: channels,
		MustNotContain: []string{"以商品详情为准", "以发货消息为准"},
		Derived:        true,
		Note:           "自动推导：提示已声明确定发货渠道，回复必须直答渠道且不得回避",
	}
}

// splitChannels 按顿号切分渠道正文并清理空白与标点。
func splitChannels(raw string) []string {
	// channels 是清理后的渠道名列表。
	channels := make([]string, 0, 4)
	// part 是当前遍历到的渠道片段。
	for _, part := range strings.Split(raw, "、") {
		// trimmed 是去掉空白与常见尾部标点后的渠道名。
		trimmed := strings.Trim(strings.TrimSpace(part), "，,；; ")
		if trimmed != "" {
			channels = append(channels, trimmed)
		}
	}
	return channels
}

// ValidateSamples 检查样本库是否可用于评测，返回可回放样本数与跳过原因。
func ValidateSamples(samples []Sample) (playable []Sample, skipped int, err error) {
	if len(samples) == 0 {
		return nil, 0, errors.New("样本库为空，请先运行 cmd/evalexport 导出事故样本")
	}
	playable = make([]Sample, 0, len(samples))
	// sample 是当前遍历到的样本。
	for _, sample := range samples {
		if sample.Replayable() {
			playable = append(playable, sample)
			continue
		}
		skipped++
	}
	if len(playable) == 0 {
		return nil, skipped, errors.New("样本库中没有可回放样本（缺少快照或买家消息）")
	}
	return playable, skipped, nil
}
