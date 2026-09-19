package result

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestOKShape(t *testing.T) {
	raw, err := json.Marshal(OK(map[string]string{"a": "b"}))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"code":0,"message":"OK","data":{"a":"b"},"success":true}`
	if string(raw) != want {
		t.Errorf("响应体 = %s, 期望 %s", raw, want)
	}
}

func TestFailShape(t *testing.T) {
	raw, err := json.Marshal(Fail("用户名或密码错误"))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"code":1,"message":"用户名或密码错误","data":null,"success":false}`
	if string(raw) != want {
		t.Errorf("响应体 = %s, 期望 %s", raw, want)
	}
}

func TestOKMessageSetsSuccess(t *testing.T) {
	got := OKMessage("自定义成功", []string{"a"})
	if !got.Success || got.Code != CodeSuccess || !got.IsSuccess() {
		t.Errorf("OKMessage = %+v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"code":0,"message":"自定义成功","data":["a"],"success":true}`
	if string(raw) != want {
		t.Errorf("响应体 = %s, 期望 %s", raw, want)
	}
}

// TestResultKeyOrder 顶层键序：code→message→data→success。
func TestResultKeyOrder(t *testing.T) {
	cases := []struct {
		name    string
		payload any
	}{
		{name: "成功", payload: OK(map[string]string{"a": "b"})},
		{name: "业务失败", payload: Fail("用户名或密码错误")},
	}
	for _, testCase := range cases {
		raw, err := json.Marshal(testCase.payload)
		if err != nil {
			t.Fatalf("%s 序列化失败: %v", testCase.name, err)
		}
		if got := strings.Join(topLevelKeys(t, raw), ","); got != "code,message,data,success" {
			t.Errorf("%s 顶层键序 = %s, 期望 code,message,data,success", testCase.name, got)
		}
	}
}

// topLevelKeys 按出现顺序返回顶层 JSON 对象的键。
func topLevelKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil { // 消费顶层 '{'
		t.Fatalf("解析响应失败: %v (%s)", err, raw)
	}
	keys := make([]string, 0, 4)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("读取顶层键失败: %v (%s)", err, raw)
		}
		key, ok := token.(string)
		if !ok {
			t.Fatalf("顶层键不是字符串: %v (%s)", token, raw)
		}
		keys = append(keys, key)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("跳过键 %s 的值失败: %v (%s)", key, err, raw)
		}
	}
	return keys
}

func TestFailCode(t *testing.T) {
	// 前端模块乐观锁使用 40901。
	got := FailCode(40901, "数据已被他人修改")
	if got.Code != 40901 || got.Data != nil {
		t.Errorf("FailCode = %+v", got)
	}
	// 非 0 业务码一律 success=false。
	if got.Success || got.IsSuccess() {
		t.Errorf("FailCode 的 success 应为 false: %+v", got)
	}
}

func TestPageDataFieldNames(t *testing.T) {
	raw, err := json.Marshal(NewPageData([]string{"a"}, 3, 1, 10))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// 普通分页 headNodeTotal=null
	want := `{"total":3,"headNodeTotal":null,"pageIndex":1,"pageSize":10,"list":["a"]}`
	if string(raw) != want {
		t.Errorf("分页响应体 = %s, 期望 %s", raw, want)
	}
	// 树分页 headNodeTotal 有值
	treeRaw, err := json.Marshal(NewTreePageData([]string{"a"}, 3, 2, 1, 10))
	if err != nil {
		t.Fatalf("树分页序列化失败: %v", err)
	}
	treeWant := `{"total":3,"headNodeTotal":2,"pageIndex":1,"pageSize":10,"list":["a"]}`
	if string(treeRaw) != treeWant {
		t.Errorf("树分页响应体 = %s, 期望 %s", treeRaw, treeWant)
	}
}

func TestPageDataNilListBecomesEmptyArray(t *testing.T) {
	raw, err := json.Marshal(NewPageData[string](nil, 0, 1, 10))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"total":0,"headNodeTotal":null,"pageIndex":1,"pageSize":10,"list":[]}`
	if string(raw) != want {
		t.Errorf("空列表响应体 = %s, 期望 %s", raw, want)
	}
}

func TestNormalizeAndOffset(t *testing.T) {
	if idx, size := NormalizePage(0, 0); idx != 1 || size != 10 {
		t.Errorf("NormalizePage(0,0) = %d,%d", idx, size)
	}
	if idx, size := NormalizePage(-3, 500); idx != 1 || size != 100 {
		t.Errorf("NormalizePage(-3,500) = %d,%d", idx, size)
	}
	if got := Offset(3, 10); got != 20 {
		t.Errorf("Offset(3,10) = %d", got)
	}
	if got := Offset(0, 0); got != 0 {
		t.Errorf("Offset(0,0) = %d", got)
	}
}
