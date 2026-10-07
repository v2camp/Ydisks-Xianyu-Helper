package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"xianyu-go/internal/netguard"
)

// ContentHit 描述发货内容扫描命中的一条门禁规则；Detail 只含规则标签与命中词，不回显用户正文或卡密。
type ContentHit struct {
	// Rule 是命中规则类别，例如联系方式/引流、非网盘外链、内置违禁词或额外违禁词。
	Rule string
	// Detail 是可直接放进错误消息的命中说明，不含敏感正文。
	Detail string
}

// LinkHealth 描述单个链接健康检查的判定档位。
type LinkHealth string

const (
	// LinkHealthOK 表示链接返回 2xx/3xx，可以随发货内容发出。
	LinkHealthOK LinkHealth = "ok"
	// LinkHealthSuspect 表示链接返回其他状态码（网盘反爬常 403），只记录日志不拦截。
	LinkHealthSuspect LinkHealth = "suspect"
	// LinkHealthDead 表示链接不可达或明确失效（网络错误/超时/DNS 失败/404/410），必须拦截。
	LinkHealthDead LinkHealth = "dead"
)

// LinkHit 保存单个被检查 URL 的健康判定结果。
type LinkHit struct {
	// URL 是文本中提取出的待发送地址原文。
	URL string
	// Health 是该地址的健康判定档位。
	Health LinkHealth
	// StatusCode 是探活收到的 HTTP 状态码；请求未完成时为 0。
	StatusCode int
	// Detail 是判定说明，只含主机名与状态语义，不含提取码等链接参数。
	Detail string
}

// DeliveryGuardConfig 保存发货内容门禁的运行时开关与额外违禁词。
type DeliveryGuardConfig struct {
	// Enabled 表示整个发货内容门禁是否生效；显式 false 时扫描与链接检查全部短路。
	Enabled bool
	// LinkCheck 表示是否在发送前对文本里的 http(s) 链接做健康检查。
	LinkCheck bool
	// ExtraBlockWords 是管理员追加的违禁词，逗号或竖线分隔。
	ExtraBlockWords string
}

// errContentBlocked 标记发货文本命中违禁词或非网盘外链门禁；确定未发送且禁止自动重试。
var errContentBlocked = errors.New("发货内容门禁拦截")

// errLinkDead 标记发货文本中的链接健康检查判定为不可达；确定未发送且禁止自动重试。
var errLinkDead = errors.New("链接健康检查拦截")

// deliveryContentGuardSettingKey 是系统设置里发货内容门禁配置的键名。
const deliveryContentGuardSettingKey = "delivery_content_guard"

// deliveryLinkCheckMaxURLs 限制单次文本链接健康检查的 URL 数量，避免一次发送拖垮自动化工作线程。
const deliveryLinkCheckMaxURLs = 5

// deliveryLinkCheckTimeout 是单个链接 HEAD/GET 探活请求的超时时间；测试可临时缩短它以验证超时分支。
var deliveryLinkCheckTimeout = 5 * time.Second

// newDeliveryLinkHTTPClient 构造链接探活客户端；生产固定走 netguard 出站策略，测试可注入本地替身。
var newDeliveryLinkHTTPClient = func(timeout time.Duration) *http.Client {
	return netguard.ConfiguredHTTPClient(timeout)
}

// deliveryContactPatterns 是联系方式与站外引流的命中词表（小写匹配）；文案标签直接用于命中说明。
var deliveryContactPatterns = []string{"微信", "加微", "加v", "维信", "薇信", "vx", "qq", "二维码", "扫码", "站外", "线下交易", "加我"}

// deliveryBuiltinBlockWords 是内置违禁词短表，覆盖侵权、色情、灰产等闲鱼硬红线品类。
var deliveryBuiltinBlockWords = []string{"侵权", "盗版", "色情", "代孕", "枪支", "毒品", "赌博", "诈骗", "办证", "发票"}

// netdiskHostExact 是不以 pan./yun./drive. 开头也放行的网盘/云盘根域名。
var netdiskHostExact = map[string]bool{
	"cloud.189.cn":        true,
	"alipan.com":          true,
	"www.alipan.com":      true,
	"www.aliyundrive.com": true,
	"www.iqiyi.com":       true,
}

// deliveryURLPattern 匹配 http(s):// 或 www. 开头的 URL 候选；空白与中英文标点不进入候选。
var deliveryURLPattern = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>\[\]()（）【】{}，。；、！？"'“”‘’]+`)

// deliveryURLTrailingCutset 是 URL 候选末尾常被夹带的成对符号或句读，提取时裁掉。
const deliveryURLTrailingCutset = ".,;:!?~)]}'\"“”‘’"

// ScanDeliveryContent 执行内置发货内容规则扫描：联系方式/引流、非网盘外链与内置违禁词。
// 参数 text 是即将发给买家的最终文本；返回 nil 表示未命中任何内置规则。
func ScanDeliveryContent(text string) []ContentHit {
	// trimmed 是去除首尾空白后的待扫描文本；空文本没有出站内容，直接放行。
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	// lower 是小写化后的扫描副本，让 vx/QQ 等拉丁词大小写不敏感命中。
	lower := strings.ToLower(trimmed)
	// hits 按规则顺序收集全部命中，供错误消息一次列出全部原因。
	var hits []ContentHit
	// pattern 是当前联系方式/引流词表中的小写词条。
	for _, pattern := range deliveryContactPatterns {
		if strings.Contains(lower, pattern) {
			hits = append(hits, ContentHit{Rule: "联系方式/引流", Detail: "联系方式/引流:" + pattern})
		}
	}
	hits = append(hits, scanMobileNumberRuns(trimmed)...)
	// raw 是文本中提取出的 URL 候选原文。
	for _, raw := range extractURLCandidates(trimmed) {
		// host 是候选地址的小写主机名；解析失败时为空串。
		host := urlHost(raw)
		if isNetdiskHost(host) {
			continue
		}
		// shown 是命中说明里展示的域名标签；解析失败时不回显原文，避免把卡密拼接的伪 URL 写进错误。
		shown := host
		if shown == "" {
			shown = "未知域名"
		}
		hits = append(hits, ContentHit{Rule: "非网盘外链", Detail: "非网盘域名:" + shown})
	}
	// word 是内置违禁词短表中的词条。
	for _, word := range deliveryBuiltinBlockWords {
		if strings.Contains(lower, word) {
			hits = append(hits, ContentHit{Rule: "内置违禁词", Detail: "内置违禁词:" + word})
		}
	}
	return hits
}

// ScanDeliveryContentWithConfig 在内置规则之上追加设置里的额外违禁词；门禁关闭时直接放行。
// 参数 cfg 是解析后的门禁配置；返回 nil 表示门禁关闭或未命中。
func ScanDeliveryContentWithConfig(text string, cfg DeliveryGuardConfig) []ContentHit {
	if !cfg.Enabled {
		return nil
	}
	// hits 先收集内置规则命中，再追加设置额外词命中。
	hits := ScanDeliveryContent(text)
	return append(hits, scanExtraBlockWords(text, cfg.ExtraBlockWords)...)
}

// scanExtraBlockWords 按逗号或竖线拆分设置里的额外违禁词并逐词匹配；空词条忽略。
func scanExtraBlockWords(text, extra string) []ContentHit {
	// trimmed 是去除首尾空白后的额外词配置原文。
	trimmed := strings.TrimSpace(extra)
	if trimmed == "" {
		return nil
	}
	// lower 是小写化后的待扫描文本，额外词按大小写不敏感子串命中。
	lower := strings.ToLower(text)
	// hits 收集额外违禁词命中记录。
	var hits []ContentHit
	// raw 是按逗号或竖线拆分出的一个原始词条。
	for _, raw := range strings.FieldsFunc(trimmed, func(r rune) bool { return r == ',' || r == '|' }) {
		// word 是去除首尾空白后的词条；空词条表示分隔符冗余，直接跳过。
		word := strings.TrimSpace(raw)
		if word == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(word)) {
			hits = append(hits, ContentHit{Rule: "额外违禁词", Detail: "额外违禁词:" + word})
		}
	}
	return hits
}

// scanMobileNumberRuns 在文本的连续数字段里查找恰好 11 位的手机号形态。
// 只把独立的 11 位数字串判为手机号，避免更长订单号的子串被误杀。
func scanMobileNumberRuns(text string) []ContentHit {
	// hits 保存恰好 11 位连续数字的手机号命中记录。
	var hits []ContentHit
	// runStart 记录当前连续数字串的起始下标；-1 表示当前不在数字串内。
	runStart := -1
	// index 遍历文本的每个字节下标；结尾会经过一次非数字边界以收口最后一段数字段。
	for index := 0; index <= len(text); index++ {
		// isDigit 表示当前位置是否为 ASCII 数字；结尾哨兵视为非数字边界。
		isDigit := index < len(text) && text[index] >= '0' && text[index] <= '9'
		if isDigit {
			if runStart < 0 {
				runStart = index
			}
			continue
		}
		if runStart >= 0 {
			// runLength 是刚结束的连续数字串长度（字节数等于位数）。
			runLength := index - runStart
			if runLength == 11 {
				hits = append(hits, ContentHit{Rule: "联系方式/引流", Detail: "联系方式/引流:手机号"})
			}
			runStart = -1
		}
	}
	return hits
}

// extractURLCandidates 提取文本中的 http(s):// 与 www. 形态 URL 候选并裁掉末尾句读。
func extractURLCandidates(text string) []string {
	// matches 是正则匹配出的原始候选列表。
	matches := deliveryURLPattern.FindAllString(text, -1)
	// out 保存裁掉末尾句读后的候选，保持出现顺序。
	out := make([]string, 0, len(matches))
	// match 是当前候选原文。
	for _, match := range matches {
		// cleaned 是去掉末尾成对符号与句读后的地址。
		cleaned := strings.TrimRight(match, deliveryURLTrailingCutset)
		if cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

// urlHost 返回 URL 的小写主机名；www. 形态会先补全 http:// 再解析，解析失败返回空串。
func urlHost(raw string) string {
	// candidate 是补全协议后的可解析地址。
	candidate := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(candidate), "www.") {
		candidate = "http://" + candidate
	}
	// parsed、err 保存地址解析结果；解析失败时不猜测主机名。
	parsed, err := url.Parse(candidate)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// isNetdiskHost 判断主机名是否属于放行的网盘/云盘域；网盘链接是交付物本身，不做外链拦截。
// 放行条件是精确白名单，或主机名以 pan./yun./drive. 开头的网盘子域。
func isNetdiskHost(host string) bool {
	// normalized 是小写去空白后的主机名。
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return false
	}
	if netdiskHostExact[normalized] {
		return true
	}
	return strings.HasPrefix(normalized, "pan.") || strings.HasPrefix(normalized, "yun.") || strings.HasPrefix(normalized, "drive.")
}

// ParseDeliveryGuardConfig 解析系统设置里的发货内容门禁 JSON。
// 键缺失、空串或非法 JSON 一律按默认配置处理：启用扫描、启用链接检查、无额外违禁词；仅显式 false 关闭对应开关。
func ParseDeliveryGuardConfig(raw string) DeliveryGuardConfig {
	// cfg 是默认启用门禁的配置基线。
	cfg := DeliveryGuardConfig{Enabled: true, LinkCheck: true}
	// trimmed 是去除首尾空白后的设置原文；空值等同未配置。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return cfg
	}
	// parsed 保存显式配置字段；指针为 nil 表示配置未写该字段，保持默认值。
	var parsed struct {
		Enabled         *bool  `json:"enabled"`
		LinkCheck       *bool  `json:"link_check"`
		ExtraBlockWords string `json:"extra_block_words"`
	}
	if json.Unmarshal([]byte(trimmed), &parsed) != nil {
		return cfg
	}
	if parsed.Enabled != nil {
		cfg.Enabled = *parsed.Enabled
	}
	if parsed.LinkCheck != nil {
		cfg.LinkCheck = *parsed.LinkCheck
	}
	cfg.ExtraBlockWords = parsed.ExtraBlockWords
	return cfg
}

// CheckDeliveryLinks 对文本中将要发送的 http(s) URL 逐个做可达性检查，最多检查 5 个。
// 参数 ctx 是调用方取消边界；text 是即将出站的最终文本；返回结果按 URL 出现顺序去重排列。
// 本函数会发起网络 I/O，调用方必须处于无锁区。
func CheckDeliveryLinks(ctx context.Context, text string) []LinkHit {
	// urls 是去重并截断到上限后的待检查地址。
	urls := extractCheckableURLs(text)
	if len(urls) == 0 {
		return nil
	}
	if ctx == nil {
		// 历史独立动作测试允许 nil Context；探活改用带显式超时的有限收口预算，避免裸根上下文。
		// bounded 是本次探活的有限收口预算；cancel 是配套取消函数，在函数退出时释放。
		bounded, cancel := context.WithTimeout(context.Background(), deliveryLinkCheckTimeout)
		defer cancel()
		ctx = bounded
	}
	// client 按 netguard 出站策略构造，重定向与拨号都受公网策略约束。
	client := newDeliveryLinkHTTPClient(deliveryLinkCheckTimeout)
	// hits 保存各地址的健康判定结果。
	hits := make([]LinkHit, 0, len(urls))
	// raw 是当前待探活的地址原文。
	for _, raw := range urls {
		hits = append(hits, checkDeliveryLink(ctx, client, raw))
	}
	return hits
}

// extractCheckableURLs 提取文本里的 http(s):// 地址，按出现顺序去重并限制数量。
func extractCheckableURLs(text string) []string {
	// seen 记录已经入选的地址，避免同一链接被重复探活。
	seen := make(map[string]struct{})
	// out 保存最终待检查地址列表。
	out := make([]string, 0, deliveryLinkCheckMaxURLs)
	// raw 是文本中提取出的 URL 候选原文。
	for _, raw := range extractURLCandidates(text) {
		// lower 是用于去重比较的小写地址。
		lower := strings.ToLower(raw)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			continue
		}
		// exists 表示该小写地址是否已经入选，重复链接只探活一次。
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, raw)
		if len(out) >= deliveryLinkCheckMaxURLs {
			break
		}
	}
	return out
}

// checkDeliveryLink 用 HEAD（失败降级 GET）探测单个地址并给出健康判定。
func checkDeliveryLink(ctx context.Context, client *http.Client, raw string) LinkHit {
	// response、err 保存 HEAD 探活的响应与传输错误。
	response, err := doDeliveryLinkRequest(ctx, client, http.MethodHead, raw)
	// headUnsupported 表示服务端明确不支持 HEAD，需要降级 GET 再判。
	headUnsupported := err == nil && (response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented)
	if headUnsupported {
		closeDeliveryLinkResponse(response)
		response, err = nil, errors.New("服务端不支持 HEAD，改用 GET 探活")
	}
	if err != nil {
		// HEAD 传输失败时降级 GET 再判一次，兼容只实现 GET 的网盘分享页。
		response, err = doDeliveryLinkRequest(ctx, client, http.MethodGet, raw)
	}
	if err != nil {
		return LinkHit{URL: raw, Health: LinkHealthDead, Detail: "链接请求失败或超时"}
	}
	// code 是本次探活最终采用的 HTTP 状态码。
	code := response.StatusCode
	closeDeliveryLinkResponse(response)
	// host 是用于命中说明的小写主机名，不包含分享码等路径参数。
	host := urlHost(raw)
	switch {
	case code >= 200 && code < 400:
		return LinkHit{URL: raw, Health: LinkHealthOK, StatusCode: code, Detail: "链接可达(host=" + host + ")"}
	case code == http.StatusNotFound || code == http.StatusGone:
		return LinkHit{URL: raw, Health: LinkHealthDead, StatusCode: code, Detail: "链接已失效(host=" + host + ")"}
	default:
		return LinkHit{URL: raw, Health: LinkHealthSuspect, StatusCode: code, Detail: "链接状态存疑(host=" + host + ")"}
	}
}

// doDeliveryLinkRequest 发起一次带取消上下文的链接探活请求；调用方负责关闭响应体。
func doDeliveryLinkRequest(ctx context.Context, client *http.Client, method, raw string) (*http.Response, error) {
	// request 是构造出的探活请求；非法地址或空上下文会在此失败。
	request, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

// closeDeliveryLinkResponse 读走少量响应体后关闭连接，避免探活请求泄漏连接。
func closeDeliveryLinkResponse(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	// buffer 用于吸收响应体开头的少量字节，帮助连接被正确释放。
	var buffer [256]byte
	// 读取失败不影响探活结论，随后统一关闭。
	_, _ = response.Body.Read(buffer[:])
	_ = response.Body.Close()
}

// formatContentHits 把命中记录拼成错误消息片段；片段只含规则标签，不含用户正文。
func formatContentHits(hits []ContentHit) string {
	// parts 保存逐条命中说明。
	parts := make([]string, 0, len(hits))
	// hit 是当前命中记录。
	for _, hit := range hits {
		parts = append(parts, hit.Rule+":"+hit.Detail)
	}
	return strings.Join(parts, "; ")
}

// guardSendError 组装门禁拦截错误：标记确定未发送与不可重试，并携带仅含规则标签的命中原因。
func guardSendError(sentinel error, reason string) error {
	// base 保留确定未发送与门禁哨兵错误链，供库存恢复和人工核对路径识别。
	base := fmt.Errorf("%w: %w: %s", ErrMessageNotSent, sentinel, reason)
	return noRetryAction(base)
}

// isDeliveryGuardError 判断错误是否为发货内容门禁或链接健康检查拦截。
func isDeliveryGuardError(err error) bool {
	return errors.Is(err, errContentBlocked) || errors.Is(err, errLinkDead)
}

// loadDeliveryGuardConfig 读取系统设置里的发货内容门禁配置；读取失败按默认启用处理，不允许静默放行。
func (e *automationActionExecutor) loadDeliveryGuardConfig(ctx context.Context) DeliveryGuardConfig {
	if e.store == nil || e.store.Settings == nil {
		return ParseDeliveryGuardConfig("")
	}
	// raw、err 保存设置原文及读取错误；错误时按默认门禁继续，宁可多拦也不漏拦。
	raw, err := e.store.Settings.Get(ctx, deliveryContentGuardSettingKey)
	if err != nil {
		return ParseDeliveryGuardConfig("")
	}
	return ParseDeliveryGuardConfig(raw)
}

// guardOutboundText 在出站文本发送前执行违禁词/外链门禁与链接健康检查。
// 参数 accountID 只用于日志定位；命中返回确定未发送、不可重试的门禁错误，错误消息只含规则标签。
// 本方法会发起网络 I/O，调用方必须处于无锁区。
func (e *automationActionExecutor) guardOutboundText(ctx context.Context, accountID, text string) error {
	// cfg 保存当前生效的门禁配置；显式关闭时整个门禁短路。
	cfg := e.loadDeliveryGuardConfig(ctx)
	if !cfg.Enabled {
		return nil
	}
	// hits 保存内置规则与设置额外违禁词的命中记录。
	hits := ScanDeliveryContentWithConfig(text, cfg)
	if len(hits) > 0 {
		return guardSendError(errContentBlocked, formatContentHits(hits))
	}
	if !cfg.LinkCheck {
		return nil
	}
	// linkHits 保存文本中将要发送 URL 的健康检查结果。
	linkHits := CheckDeliveryLinks(ctx, text)
	// hit 是当前链接判定记录。
	for _, hit := range linkHits {
		switch hit.Health {
		case LinkHealthDead:
			return guardSendError(errLinkDead, hit.Detail)
		case LinkHealthSuspect:
			if e.logger != nil {
				e.logger.Warn("发货链接状态存疑，仅记录不拦截", "account", accountID, "detail", hit.Detail, "status_code", hit.StatusCode)
			}
		}
	}
	return nil
}
