package result

import (
	"encoding/json"
	"testing"
)

func TestOKShape(t *testing.T) {
	raw, err := json.Marshal(OK(map[string]string{"a": "b"}))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"code":0,"message":"OK","data":{"a":"b"}}`
	if string(raw) != want {
		t.Errorf("响应体 = %s, 期望 %s", raw, want)
	}
}

func TestFailShape(t *testing.T) {
	raw, err := json.Marshal(Fail("用户名或密码错误"))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"code":1,"message":"用户名或密码错误","data":null}`
	if string(raw) != want {
		t.Errorf("响应体 = %s, 期望 %s", raw, want)
	}
}

func TestFailCode(t *testing.T) {
	// 前端模块乐观锁使用 40901，是 Java 侧唯一的非 1 业务码。
	got := FailCode(40901, "数据已被他人修改")
	if got.Code != 40901 || got.Data != nil {
		t.Errorf("FailCode = %+v", got)
	}
}

func TestPageDataFieldNames(t *testing.T) {
	raw, err := json.Marshal(NewPageData([]string{"a"}, 3, 1, 10))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"total":3,"headNodeTotal":3,"pageIndex":1,"pageSize":10,"list":["a"]}`
	if string(raw) != want {
		t.Errorf("分页响应体 = %s, 期望 %s", raw, want)
	}
}

func TestPageDataNilListBecomesEmptyArray(t *testing.T) {
	raw, err := json.Marshal(NewPageData[string](nil, 0, 1, 10))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	want := `{"total":0,"headNodeTotal":0,"pageIndex":1,"pageSize":10,"list":[]}`
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
