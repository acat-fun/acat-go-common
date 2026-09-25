package satoken

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadJavaWrittenSession 用 Sa-Token 1.44.0 产出的会话 JSON（testdata 样本）
// 验证解码：这是会话互认最关键的一条兼容性回归
// （样本由 interop.SaTokenInterop 在 Sa-Token 1.44.0 + sa-token-jackson 下产出）。
func TestReadJavaWrittenSession(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "satoken-java-session.json"))
	if err != nil {
		t.Fatalf("读取样本失败: %v", err)
	}
	session, err := unmarshalSession(raw)
	if err != nil {
		t.Fatalf("解析 Java 会话失败: %v", err)
	}
	if session.ID != "11111111-2222-4333-8444-555555555555" {
		t.Errorf("id = %s", session.ID)
	}
	if session.Type != SessionTypeAccount || session.LoginType != "login" {
		t.Errorf("type/loginType = %s/%s", session.Type, session.LoginType)
	}
	if loginIDOf(session) != "0" {
		t.Errorf("loginId = %v", session.LoginID)
	}
	if session.HistoryTerminalCount != 1 || session.CreateTime != 1789317424024 {
		t.Errorf("historyTerminalCount/createTime = %d/%d", session.HistoryTerminalCount, session.CreateTime)
	}
	if len(session.TerminalList) != 1 {
		t.Fatalf("terminalList = %+v", session.TerminalList)
	}
	terminal := session.TerminalList[0]
	if terminal.TokenValue != "java-e2e-token-1" || terminal.DeviceType != "DEF" || terminal.Index != 1 {
		t.Errorf("terminal = %+v", terminal)
	}
	// dataMap 是 WRAPPER_ARRAY + ImmutableCollections 包装，必须解出字符串列表。
	perms := session.StringList(DataKeyPermissions)
	if len(perms) != 2 || perms[0] != "acat:admin:system:dicts" || perms[1] != "acat:admin:system:dicts:create" {
		t.Errorf("permissions = %v", perms)
	}
	if roles := session.StringList(DataKeyRoles); len(roles) != 1 || roles[0] != "root" {
		t.Errorf("roles = %v", roles)
	}
	if got := session.String(DataKeyUsername); got != "root" {
		t.Errorf("username = %s", got)
	}
}

// TestMarshalSessionMatchesJavaShape 锁定 Go 写出的 JSON 形态与 Java 一致：
// 集合用 WRAPPER_ARRAY，SaSession/SaTerminalInfo 用 @class 属性。
func TestMarshalSessionMatchesJavaShape(t *testing.T) {
	session := NewSession("11111111-2222-4333-8444-555555555555")
	session.Type = SessionTypeAccount
	session.LoginType = "login"
	session.LoginID = "0"
	session.CreateTime = 1789317424024
	// 与
	session.AddTerminal(TerminalInfo{TokenValue: "go-token-1", CreateTime: 1789317424025})
	session.Set(DataKeyPermissions, []string{"acat:admin:system:dicts"})
	session.Set(DataKeyUsername, "root")

	raw, err := marshalSession(session)
	if err != nil {
		t.Fatalf("marshalSession 失败: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if decoded["@class"] != javaClassSession {
		t.Errorf("@class = %v", decoded["@class"])
	}
	if decoded["historyTerminalCount"] != float64(1) {
		t.Errorf("historyTerminalCount = %v", decoded["historyTerminalCount"])
	}
	if decoded["token"] != nil {
		t.Errorf("token 应为 null，实际 %v", decoded["token"])
	}

	// dataMap 必须是 WRAPPER_ARRAY：["java.util.concurrent.ConcurrentHashMap", {...}]
	dataMap, ok := decoded["dataMap"].([]any)
	if !ok || len(dataMap) != 2 {
		t.Fatalf("dataMap 应为 WRAPPER_ARRAY，实际 %T", decoded["dataMap"])
	}
	if dataMap[0] != javaClassSessionMap {
		t.Errorf("dataMap[0] = %v, 期望 %s", dataMap[0], javaClassSessionMap)
	}
	inner, ok := dataMap[1].(map[string]any)
	if !ok {
		t.Fatalf("dataMap[1] 应为对象，实际 %T", dataMap[1])
	}
	if inner["username"] != "root" {
		t.Errorf("dataMap.username = %v", inner["username"])
	}
	permWrapper, ok := inner[DataKeyPermissions].([]any)
	if !ok || len(permWrapper) != 2 || permWrapper[0] != javaClassListWrapper {
		t.Fatalf("permissions 应为 WRAPPER_ARRAY，实际 %v", inner[DataKeyPermissions])
	}

	// terminalList 必须是 ["java.util.Vector", [ {..SaTerminalInfo..} ]]
	terminals, ok := decoded["terminalList"].([]any)
	if !ok || len(terminals) != 2 || terminals[0] != javaClassTerminalVector {
		t.Fatalf("terminalList 应为 Vector WRAPPER_ARRAY，实际 %v", decoded["terminalList"])
	}
	items, ok := terminals[1].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("terminalList 元素异常: %v", terminals[1])
	}
	first, _ := items[0].(map[string]any)
	if first["@class"] != javaClassTerminalInfo {
		t.Errorf("terminal @class = %v", first["@class"])
	}
	if first["index"] != float64(1) || first["deviceType"] != "DEF" {
		t.Errorf("terminal 字段 = %v", first)
	}

	// Go 自产 JSON 必须能被自己解析（幂等）。
	back, err := unmarshalSession(raw)
	if err != nil {
		t.Fatalf("round-trip 失败: %v", err)
	}
	if back.HistoryTerminalCount != 1 || len(back.TerminalList) != 1 || back.TerminalList[0].TokenValue != "go-token-1" {
		t.Errorf("round-trip 结果 = %+v", back)
	}
	if got := back.StringList(DataKeyPermissions); len(got) != 1 || got[0] != "acat:admin:system:dicts" {
		t.Errorf("round-trip permissions = %v", got)
	}
	if !strings.Contains(string(raw), `"@class":"cn.dev33.satoken.session.SaTerminalInfo"`) {
		t.Errorf("缺少 SaTerminalInfo 类型标记: %s", raw)
	}
}

// TestUnmarshalLegacyPropertyWrapper 兼容早期 Go 实现的 {"@class":...,"@items":[...]} 形态。
func TestUnmarshalLegacyPropertyWrapper(t *testing.T) {
	legacy := `{
	  "@class": "cn.dev33.satoken.session.SaSession",
	  "id": "s-1", "type": "Account-Session", "loginType": "login", "loginId": "0",
	  "createTime": 1, "terminalList": null,
	  "dataMap": {"@class": "java.util.LinkedHashMap",
	    "permissions": {"@class": "java.util.ArrayList", "@items": ["a", "b"]},
	    "username": "root"}
	}`
	session, err := unmarshalSession([]byte(legacy))
	if err != nil {
		t.Fatalf("解析早期形态失败: %v", err)
	}
	if got := session.StringList(DataKeyPermissions); len(got) != 2 || got[1] != "b" {
		t.Errorf("permissions = %v", got)
	}
	if session.String(DataKeyUsername) != "root" {
		t.Errorf("username = %s", session.String(DataKeyUsername))
	}
}

// TestAddTerminalAssignsIndexAndCount 锁定终端计数语义。
func TestAddTerminalAssignsIndexAndCount(t *testing.T) {
	session := NewSession("s")
	session.AddTerminal(TerminalInfo{TokenValue: "t1"})
	session.AddTerminal(TerminalInfo{TokenValue: "t2"})
	if session.HistoryTerminalCount != 2 {
		t.Errorf("historyTerminalCount = %d", session.HistoryTerminalCount)
	}
	if session.TerminalList[0].Index != 1 || session.TerminalList[1].Index != 2 {
		t.Errorf("index 分配 = %d/%d", session.TerminalList[0].Index, session.TerminalList[1].Index)
	}
	if session.TerminalList[0].DeviceType != "DEF" {
		t.Errorf("deviceType 默认值 = %s", session.TerminalList[0].DeviceType)
	}
	// 重复 token 不新增。
	session.AddTerminal(TerminalInfo{TokenValue: "t1"})
	if len(session.TerminalList) != 2 || session.HistoryTerminalCount != 2 {
		t.Errorf("重复 token 应被忽略: %+v", session.TerminalList)
	}
	// 删除终端不回退计数。
	session.RemoveTerminal("t1")
	if len(session.TerminalList) != 1 || session.HistoryTerminalCount != 2 {
		t.Errorf("删除后 = %d/%d", len(session.TerminalList), session.HistoryTerminalCount)
	}
}

// TestUnmarshalNumberLoginID 覆盖 loginId 被写成数字的历史形态。
func TestUnmarshalNumberLoginID(t *testing.T) {
	fixture := `{"id":"s1","type":"Account-Session","loginType":"login","loginId":123456,"dataMap":{}}`
	session, err := unmarshalSession([]byte(fixture))
	if err != nil {
		t.Fatalf("unmarshalSession 失败: %v", err)
	}
	if loginIDOf(session) != "123456" {
		t.Errorf("loginId = %q", loginIDOf(session))
	}
}
