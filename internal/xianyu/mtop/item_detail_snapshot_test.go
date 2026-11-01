package mtop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchItemDetailSnapshotParsesFacts 验证单商品回补详情按平台字段解析出标题、价格、描述与发布人。
func TestFetchItemDetailSnapshotParsesFacts(t *testing.T) {
	// server 是返回固定详情响应的本地 HTTP 替身，隔离平台网络。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ret":["SUCCESS::调用成功"],"data":{`+
			`"itemDO":{"itemId":"item-9","title":"测试标题","desc":"测试描述","categoryId":"5001",`+
			`"priceInfo":{"price":12.5,"preText":"￥"},"skuList":[{"id":"1"},{"id":"2"}]},`+
			`"sellerDO":{"sellerId":"seller-self"}}}`)
	}))
	defer server.Close()
	// client 指向本地替身，仅覆盖详情端点。
	client := &ClientImpl{HTTPClient: server.Client(), ItemDetailURL: server.URL}
	// snapshot、err 保存解析结果及请求错误。
	snapshot, err := client.FetchItemDetailSnapshot(context.Background(), "_m_h5_tk=token_1", "item-9")
	if err != nil {
		t.Fatalf("读取商品详情快照失败: %v", err)
	}
	if snapshot.ItemID != "item-9" || snapshot.Title != "测试标题" || snapshot.Description != "测试描述" {
		t.Fatalf("商品基础事实解析异常: %+v", snapshot)
	}
	if snapshot.CategoryID != "5001" || snapshot.Price != "12.5" || snapshot.PriceText != "￥12.5" {
		t.Fatalf("商品价格或类目解析异常: %+v", snapshot)
	}
	if snapshot.SellerID != "seller-self" || !snapshot.IsMultiSpec {
		t.Fatalf("发布人或多规格事实解析异常: %+v", snapshot)
	}
	// detail 是写回本地商品表前的扩展详情 JSON，必须可解析且包含网页地址。
	var detail map[string]any
	if // err 是扩展详情 JSON 解析错误。
	err := json.Unmarshal([]byte(snapshot.DetailJSON), &detail); err != nil {
		t.Fatalf("扩展详情 JSON 不可解析: %v", err)
	}
	if detail["title"] != "测试标题" || detail["web_url"] != "https://www.goofish.com/item?id=item-9" {
		t.Fatalf("扩展详情 JSON 内容异常: %v", detail)
	}
}

// TestFetchItemDetailSnapshotKeepsEmptySeller 验证详情缺少发布人时返回空发布人，由调用方拒绝落库。
func TestFetchItemDetailSnapshotKeepsEmptySeller(t *testing.T) {
	// server 返回缺少 sellerDO 的详情响应。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ret":["SUCCESS::调用成功"],"data":{"itemDO":{"title":"无发布人"}}}`)
	}))
	defer server.Close()
	// client 指向本地替身。
	client := &ClientImpl{HTTPClient: server.Client(), ItemDetailURL: server.URL}
	// snapshot、err 保存解析结果；缺少发布人不属于请求错误，交由适配层做归属判断。
	snapshot, err := client.FetchItemDetailSnapshot(context.Background(), "_m_h5_tk=token_1", "item-10")
	if err != nil {
		t.Fatalf("缺少发布人不应产生请求错误: %v", err)
	}
	if snapshot.SellerID != "" || snapshot.Title != "无发布人" || snapshot.IsMultiSpec {
		t.Fatalf("缺少发布人的解析结果异常: %+v", snapshot)
	}
}
