package adapter

import (
	"context"
	"strings"
	"testing"
)

// TestLoadBatchPublishImagesConvertsLocalAndRemoteRefs 验证本地与远程图片回调均转换为平台模型。
func TestLoadBatchPublishImagesConvertsLocalAndRemoteRefs(t *testing.T) {
	// localCalls、remoteCalls 记录两类图片读取回调的调用次数。
	localCalls, remoteCalls := 0, 0
	// readLocal 返回本地图片测试数据和稳定文件名。
	readLocal := func(_ string, ref string) ([]byte, string, string, error) {
		localCalls++
		return []byte("local"), "image/png", ref, nil
	}
	// downloadRemote 返回远程图片测试数据。
	downloadRemote := func(_ context.Context, _ string) ([]byte, string, error) {
		remoteCalls++
		return []byte("remote"), "image/jpeg", nil
	}
	// images、loadErr 保存图片转换结果和执行错误。
	images, loadErr := LoadBatchPublishImages(context.Background(), "/tmp/upload", `["local.png","https://example.test/remote.jpg?x=1"]`, readLocal, downloadRemote)
	if loadErr != nil {
		t.Fatalf("加载图片失败: %v", loadErr)
	}
	if len(images) != 2 || localCalls != 1 || remoteCalls != 1 {
		t.Fatalf("图片回调或结果异常: images=%+v local=%d remote=%d", images, localCalls, remoteCalls)
	}
	if images[1].Filename != "remote.jpg" || images[1].ContentType != "image/jpeg" {
		t.Fatalf("远程图片元数据异常: %+v", images[1])
	}
}

// TestLoadBatchPublishImagesRejectsInvalidInput 验证缺失端口、非法 JSON 和空图片列表均明确失败。
func TestLoadBatchPublishImagesRejectsInvalidInput(t *testing.T) {
	// readLocal、downloadRemote 是不应被非法输入调用的测试回调。
	readLocal := func(string, string) ([]byte, string, string, error) { return nil, "", "", nil }
	// downloadRemote 是远程图片测试回调。
	downloadRemote := func(context.Context, string) ([]byte, string, error) { return nil, "", nil }
	// cases 保存不同非法输入及其预期错误断言。
	cases := []struct {
		name       string
		imagesJSON string
		readLocal  ReadPublishImageFile
		wantErr    bool
	}{
		{name: "missing port", imagesJSON: `[]`, wantErr: true},
		{name: "bad json", imagesJSON: "not-json", readLocal: readLocal, wantErr: true},
		{name: "empty", imagesJSON: `[]`, readLocal: readLocal, wantErr: true},
	}
	// testCase 描述当前非法图片输入分支。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// remote 是当前分支可用的远程下载回调，缺失端口分支故意传入 nil。
			remote := downloadRemote
			if testCase.readLocal == nil {
				remote = nil
			}
			// _, loadErr 保存当前非法输入的执行错误。
			_, loadErr := LoadBatchPublishImages(context.Background(), "/tmp/upload", testCase.imagesJSON, testCase.readLocal, remote)
			if (loadErr != nil) != testCase.wantErr {
				t.Fatalf("错误断言不符: %v", loadErr)
			}
		})
	}
}

// TestPublishImageFilenameShortensLongURLBase 验证超长图床文件名被压到平台上限并保留扩展名。
func TestPublishImageFilenameShortensLongURLBase(t *testing.T) {
	// longURL 是被平台以 FILE_NAME_LENGTH_IS_OVER_RANGE 拒绝过的真实形态：文件名 62 字符。
	longURL := "http://img.alicdn.com/bao/uploaded/i4/11873716/O1CN01xa3LJHa8mSK2XrKe_!!4611686018427383220-0-xy_item.jpg"
	// name 是短期化后的上传文件名。
	name := PublishImageFilename(longURL)
	// length 是截断后文件名的字符数，必须落在平台上限内。
	length := len([]rune(name))
	if length > maxPublishImageFilenameRunes {
		t.Fatalf("文件名仍超上限: %d 字符 %q", length, name)
	}
	if !strings.HasSuffix(name, ".jpg") {
		t.Fatalf("扩展名必须保留: %q", name)
	}
	// other 是同前缀不同后缀的另一张图片，截断后必须仍然可区分。
	other := PublishImageFilename("http://img.alicdn.com/bao/uploaded/i4/11873716/O1CN01xa3LJHa8mSK2XrKe_!!4611686018427383220-0-xy_item2.jpg")
	if other == name {
		t.Fatalf("不同图片截断后不得同名: %q", name)
	}
	// shortURL 是短期化后仍短于上限的地址，必须原样保留。
	if got := PublishImageFilename("https://cdn.test/a.png?x=1"); got != "a.png" {
		t.Fatalf("短文件名不应改写: %q", got)
	}
}

// TestShortenPublishImageFilenameEdgeCases 验证空主干、无扩展名与超长扩展名的截断行为。
func TestShortenPublishImageFilenameEdgeCases(t *testing.T) {
	// cases 保存不同边界文件名与其断言。
	cases := []struct {
		name string
		in   string
	}{
		{name: "no extension", in: strings.Repeat("x", 80)},
		{name: "long extension", in: "a." + strings.Repeat("y", 20)},
		{name: "dots only stem", in: strings.Repeat(".", 40) + "png"},
	}
	// testCase 是当前边界场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 是截断结果，必须非空且不超上限。
			got := shortenPublishImageFilename(testCase.in)
			if got == "" || len([]rune(got)) > maxPublishImageFilenameRunes {
				t.Fatalf("截断结果非法: %q", got)
			}
		})
	}
}
