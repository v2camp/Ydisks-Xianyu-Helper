package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLooksLikeDeliverableContent 覆盖交付内容特征识别的命中与边界。
func TestLooksLikeDeliverableContent(t *testing.T) {
	// cases 保存待测文本与期望的命中结果。
	cases := []struct {
		// input 是待判定的文本。
		input string
		// want 表示该文本是否应判定为含可发送交付内容。
		want bool
	}{
		{"", false},
		{"   ", false},
		{"感谢购买", false},
		{"百度网盘教程", false},
		{"链接:https://pan.baidu.com/s/abc", true},
		{"https://example.com/x", true},
		{"www.baidu.com", true},
		{"提取码：abcd", true},
		{"密码: 1234", true},
		{"pwd=rs7a", true},
		{"见描述里的 pan.baidu 链接", true},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		// got 保存当前文本的判定结果。
		got := looksLikeDeliverableContent(c.input)
		if got != c.want {
			t.Errorf("looksLikeDeliverableContent(%q) = %v; want %v", c.input, got, c.want)
		}
	}
}

// TestBatchCreateCardsRejectsSwappedColumns 复现「内容」与「描述」填反的真实故障：
// 文本卡密的链接被放进「描述」而「内容」只放了标题，导入时必须被明确拒绝，
// 避免买家静默收不到交付内容。
func TestBatchCreateCardsRejectsSwappedColumns(t *testing.T) {
	// srv、cleanup 分别保存测试 HTTP 服务与资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 保存带认证路由的真实 HTTP 入口。
	handler := srv.Router()
	// sessionCookie 保存管理员登录后的会话凭证。
	sessionCookie := loginHelper(t, handler)

	// csv 模拟用户误填：填反卡把链接放进「描述」，正常卡把链接放进「内容」。
	csv := "名称,类型,内容,描述,启用,延迟秒,多规格,规格名,规格值\n" +
		"填反卡,text,百度网盘教程,链接:https://pan.baidu.com/s/abc?pwd=rs7a,是,0,否,,\n" +
		"正常卡,text,链接:https://pan.baidu.com/s/abc?pwd=rs7a,内部备注,是,0,否,,\n"

	// body 保存 multipart 请求的编码内容。
	var body bytes.Buffer
	// writer 负责把卡密表格写入 multipart 请求。
	writer := multipart.NewWriter(&body)
	// file 保存卡密表格文件字段。
	file, fileErr := writer.CreateFormFile("file", "cards.csv")
	if fileErr != nil {
		t.Fatalf("创建文件字段失败: %v", fileErr)
	}
	// writeErr 保存表格内容写入 multipart 文件时的错误。
	if _, writeErr := file.Write([]byte(csv)); writeErr != nil {
		t.Fatalf("写入表格失败: %v", writeErr)
	}
	// closeErr 保存 multipart 尾部边界写入错误。
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("关闭 multipart 失败: %v", closeErr)
	}

	// request 保存带有真实上传内容和认证会话的批量创建请求。
	request := httptest.NewRequest(http.MethodPost, "/cards/batch", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(sessionCookie)
	// recorder 捕获批量创建接口响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("批量上传状态=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// response 保存接口返回的逐行创建统计。
	var response cardBatchResponse
	// decodeErr 保存批量响应 JSON 解码错误。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("解码响应失败: %v", decodeErr)
	}
	if response.Created != 1 || response.Failed != 1 {
		t.Fatalf("应创建 1 个、拒绝 1 个，got created=%d failed=%d rows=%+v", response.Created, response.Failed, response.Rows)
	}

	// swapped、ok 保存被判定为填反的逐行结果。
	swapped, ok := findByRowName(response.Rows, "填反卡")
	if !ok {
		t.Fatalf("缺少填反卡的逐行结果: %+v", response.Rows)
	}
	if swapped.Success {
		t.Fatalf("填反卡应被拒绝，却创建成功: %+v", swapped)
	}
	if !strings.Contains(swapped.Error, "填反") {
		t.Fatalf("填反卡应给出明确填反提示，got error=%q", swapped.Error)
	}

	// normal、ok2 保存正常卡的逐行结果。
	normal, ok2 := findByRowName(response.Rows, "正常卡")
	if !ok2 || !normal.Success {
		t.Fatalf("正常卡应创建成功: %+v", response.Rows)
	}
}

// findByRowName 在批量结果中按名称查找逐行记录。
func findByRowName(rows []cardBatchResultRow, name string) (cardBatchResultRow, bool) {
	// r 表示当前遍历过程中的r
	for _, r := range rows {
		if r.Name == name {
			return r, true
		}
	}
	return cardBatchResultRow{}, false
}
