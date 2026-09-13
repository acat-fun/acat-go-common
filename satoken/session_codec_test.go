package satoken

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalSessionJacksonShape(t *testing.T) {
	session := &Session{
		ID:         "3f1c5b1e-0000-4000-8000-000000000001",
		Type:       SessionTypeAccount,
		LoginType:  "login",
		LoginID:    "0",
		CreateTime: 1730000000000,
	}
	session.AddTerminal(TerminalInfo{TokenValue: "tok-1", CreateTime: 1730000000001})
	session.Set(DataKeyPermissions, []string{"acat:admin:system:users:workers", "acat:admin:system:users:workers:edit"})
	session.Set(DataKeyRoles, []string{"root"})
	session.Set(DataKeyUsername, "admin")

	raw, err := marshalSession(session)
	if err != nil {
		t.Fatalf("marshalSession 失败: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if got := decoded["@class"]; got != javaClassSession {
		t.Errorf("@class = %v, 期望 %s", got, javaClassSession)
	}
	if got := decoded["loginId"]; got != "0" {
		t.Errorf("loginId = %v, 期望字符串 \"0\"", got)
	}
	if got, ok := decoded["token"]; !ok || got != nil {
		t.Errorf("无 token 时应输出 null，实际 %v（存在=%v）", got, ok)
	}
	if got := decoded["type"]; got != SessionTypeAccount {
		t.Errorf("type = %v, 期望 %s", got, SessionTypeAccount)
	}

	dataMap, ok := decoded["dataMap"].(map[string]any)
	if !ok {
		t.Fatalf("dataMap 类型异常: %T", decoded["dataMap"])
	}
	if got := dataMap["@class"]; got != javaClassLinkedMap {
		t.Errorf("dataMap.@class = %v, 期望 %s", got, javaClassLinkedMap)
	}
	permissions, ok := dataMap[DataKeyPermissions].(map[string]any)
	if !ok {
		t.Fatalf("permissions 应为带类型信息的集合包装，实际 %T", dataMap[DataKeyPermissions])
	}
	if got := permissions["@class"]; got != javaClassArrayList {
		t.Errorf("permissions.@class = %v, 期望 %s", got, javaClassArrayList)
	}
	items, ok := permissions["@items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("permissions.@items 异常: %v", permissions["@items"])
	}
	if items[0] != "acat:admin:system:users:workers" {
		t.Errorf("permissions.@items[0] = %v", items[0])
	}

	terminals, ok := decoded["terminalList"].(map[string]any)
	if !ok {
		t.Fatalf("terminalList 应为集合包装，实际 %T", decoded["terminalList"])
	}
	if got := terminals["@class"]; got != javaClassArrayList {
		t.Errorf("terminalList.@class = %v", got)
	}
	terminalItems, _ := terminals["@items"].([]any)
	if len(terminalItems) != 1 {
		t.Fatalf("terminalList 元素数 = %d, 期望 1", len(terminalItems))
	}
	first, _ := terminalItems[0].(map[string]any)
	if got := first["@class"]; got != javaClassTerminalInfo {
		t.Errorf("terminalList[0].@class = %v, 期望 %s", got, javaClassTerminalInfo)
	}
	if got := first["tokenValue"]; got != "tok-1" {
		t.Errorf("terminalList[0].tokenValue = %v", got)
	}
}

// TestUnmarshalJavaSessionFixture 用"Java 侧可能产出"的 JSON 形态验证 Go 能读；
// 该 fixture 依据 sa-token-jackson 的 default typing 约定编写，真实样本需在 UAT 交叉验证。
func TestUnmarshalJavaSessionFixture(t *testing.T) {
	fixture := `{
	  "@class": "cn.dev33.satoken.session.SaSession",
	  "id": "8b0d3b0e-1111-4222-8333-444455556666",
	  "type": "Account-Session",
	  "loginType": "login",
	  "loginId": "0",
	  "token": null,
	  "createTime": 1730000000000,
	  "terminalList": {
	    "@class": "java.util.ArrayList",
	    "@items": [
	      {"@class": "cn.dev33.satoken.session.SaTerminalInfo", "deviceType": "DEF", "tokenValue": "abc", "createTime": 1730000000001}
	    ]
	  },
	  "dataMap": {
	    "@class": "java.util.LinkedHashMap",
	    "permissions": {"@class": "java.util.ArrayList", "@items": ["acat:admin:system:dicts", "acat:admin:system:dicts:create"]},
	    "roles": {"@class": "java.util.ArrayList", "@items": ["admin"]},
	    "username": "admin"
	  }
	}`

	session, err := unmarshalSession([]byte(fixture))
	if err != nil {
		t.Fatalf("unmarshalSession 失败: %v", err)
	}
	if session.ID != "8b0d3b0e-1111-4222-8333-444455556666" {
		t.Errorf("id = %s", session.ID)
	}
	if session.Type != SessionTypeAccount || session.LoginType != "login" {
		t.Errorf("type/loginType = %s/%s", session.Type, session.LoginType)
	}
	if loginIDOf(session) != "0" {
		t.Errorf("loginId = %v", session.LoginID)
	}
	if len(session.TerminalList) != 1 || session.TerminalList[0].TokenValue != "abc" {
		t.Fatalf("terminalList 解析异常: %+v", session.TerminalList)
	}
	perms := session.StringList(DataKeyPermissions)
	if len(perms) != 2 || perms[1] != "acat:admin:system:dicts:create" {
		t.Errorf("permissions = %v", perms)
	}
	if got := session.String(DataKeyUsername); got != "admin" {
		t.Errorf("username = %s", got)
	}
	if roles := session.StringList(DataKeyRoles); len(roles) != 1 || roles[0] != "admin" {
		t.Errorf("roles = %v", roles)
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

// TestSessionRoundTrip 保证 Go 自产 JSON 能被自己解析（幂等）。
func TestSessionRoundTrip(t *testing.T) {
	session := NewSession("s-1")
	session.Type = SessionTypeAccount
	session.LoginType = "login"
	session.LoginID = "user-1"
	session.Set(DataKeyPermissions, []string{"a", "b"})
	session.AddTerminal(TerminalInfo{TokenValue: "t", CreateTime: 1})

	raw, err := marshalSession(session)
	if err != nil {
		t.Fatalf("marshalSession 失败: %v", err)
	}
	back, err := unmarshalSession(raw)
	if err != nil {
		t.Fatalf("unmarshalSession 失败: %v", err)
	}
	if back.ID != session.ID || loginIDOf(back) != "user-1" {
		t.Errorf("round-trip 基础字段不一致: %+v", back)
	}
	if got := back.StringList(DataKeyPermissions); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("round-trip permissions 不一致: %v", got)
	}
	if len(back.TerminalList) != 1 || back.TerminalList[0].TokenValue != "t" {
		t.Errorf("round-trip terminalList 不一致: %+v", back.TerminalList)
	}
	if !strings.Contains(string(raw), `"@class":"cn.dev33.satoken.session.SaSession"`) {
		t.Errorf("缺少 SaSession 类型标记: %s", raw)
	}
}
