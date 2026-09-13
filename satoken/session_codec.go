package satoken

import (
	"encoding/json"
	"fmt"
)

// TerminalInfo 对应 cn.dev33.satoken.session.SaTerminalInfo。
//
// Java 字段：deviceType、deviceId、tokenValue、extraData、createTime。
type TerminalInfo struct {
	DeviceType string `json:"deviceType,omitempty"`
	DeviceID   string `json:"deviceId,omitempty"`
	TokenValue string `json:"tokenValue,omitempty"`
	ExtraData  any    `json:"extraData,omitempty"`
	// CreateTime 毫秒时间戳（Java 侧 System.currentTimeMillis()）。
	CreateTime int64 `json:"createTime,omitempty"`
}

// MarshalJSON 输出带 Jackson 类型信息的终端信息。
func (t TerminalInfo) MarshalJSON() ([]byte, error) {
	type alias TerminalInfo
	payload := struct {
		Class string `json:"@class"`
		alias
	}{Class: javaClassTerminalInfo, alias: alias(t)}
	return json.Marshal(payload)
}

// UnmarshalJSON 兼容带/不带 @class 的终端信息。
func (t *TerminalInfo) UnmarshalJSON(data []byte) error {
	type alias TerminalInfo
	var raw struct {
		Class string `json:"@class"`
		alias
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*t = TerminalInfo(raw.alias)
	return nil
}

// Session 对应 cn.dev33.satoken.session.SaSession。
//
// JSON 形态（sa-token-jackson）：
//
//	{"@class":"cn.dev33.satoken.session.SaSession",
//	 "id":"<uuid>","type":"Account-Session","loginType":"login",
//	 "loginId":"0","token":null,"createTime":1730000000000,
//	 "terminalList":[{"@class":"cn.dev33.satoken.session.SaTerminalInfo", ...}],
//	 "dataMap":{"permissions":{"@class":"java.util.ArrayList","@items":[...]}, ...}}
type Session struct {
	ID        string
	Type      string
	LoginType string
	LoginID   any
	Token     string
	// CreateTime 毫秒时间戳。
	CreateTime int64
	// TerminalList 终端列表；空时序列化为 null（Jackson 未初始化列表同样输出 null）。
	TerminalList []TerminalInfo
	// DataMap 业务数据（permissions/roles/username 等）。
	DataMap map[string]any
}

// NewSession 构造与 Java 侧 new SaSession(id) 等价的空会话。
func NewSession(id string) *Session {
	return &Session{
		ID:       id,
		DataMap:  map[string]any{},
		TerminalList: []TerminalInfo{},
	}
}

// Get 读取 dataMap 中的值。
func (s *Session) Get(key string) (any, bool) {
	if s == nil || s.DataMap == nil {
		return nil, false
	}
	v, ok := s.DataMap[key]
	return v, ok
}

// Set 写入 dataMap。
func (s *Session) Set(key string, value any) {
	if s.DataMap == nil {
		s.DataMap = map[string]any{}
	}
	s.DataMap[key] = value
}

// StringList 读取 dataMap 中的字符串列表，兼容 []string、[]any 与带类型信息的包装。
func (s *Session) StringList(key string) []string {
	v, ok := s.Get(key)
	if !ok {
		return nil
	}
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if sv, ok := item.(string); ok {
				out = append(out, sv)
			}
		}
		return out
	default:
		return nil
	}
}

// String 读取 dataMap 中的字符串值。
func (s *Session) String(key string) string {
	v, ok := s.Get(key)
	if !ok {
		return ""
	}
	if sv, ok := v.(string); ok {
		return sv
	}
	return ""
}

// marshalSession 生成与 sa-token-jackson 对齐的 JSON：
// 启用 default typing 后，非 final 类（SaSession/SaTerminalInfo/LinkedHashMap/ArrayList）
// 均以 "@class" 属性标记类型。
func marshalSession(s *Session) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("session 不能为空")
	}
	type sessionAlias struct {
		ID           string         `json:"id"`
		Type         string         `json:"type,omitempty"`
		LoginType    string         `json:"loginType,omitempty"`
		LoginID      any            `json:"loginId"`
		Token        any            `json:"token"`
		CreateTime   int64          `json:"createTime"`
		TerminalList any            `json:"terminalList"`
		DataMap      map[string]any `json:"dataMap"`
	}
	terminals := any(nil)
	if len(s.TerminalList) > 0 {
		items := make([]any, 0, len(s.TerminalList))
		for _, t := range s.TerminalList {
			items = append(items, wrapListElement(t))
		}
		terminals = map[string]any{jacksonClass: javaClassArrayList, "@items": items}
	}
	var token any
	if s.Token != "" {
		token = s.Token
	}
	payload := struct {
		Class string `json:"@class"`
		sessionAlias
	}{
		Class:        javaClassSession,
		sessionAlias: sessionAlias{
			ID:           s.ID,
			Type:         s.Type,
			LoginType:    s.LoginType,
			LoginID:      s.LoginID,
			Token:        token,
			CreateTime:   s.CreateTime,
			TerminalList: terminals,
			DataMap:      wrapDataMap(s.DataMap),
		},
	}
	return json.Marshal(payload)
}

// wrapDataMap 按 Jackson 规则包装 dataMap：map 本身带 @class，集合值额外带 @items。
func wrapDataMap(data map[string]any) map[string]any {
	out := map[string]any{jacksonClass: javaClassLinkedMap}
	for k, v := range data {
		switch typed := v.(type) {
		case []string:
			items := make([]any, 0, len(typed))
			for _, s := range typed {
				items = append(items, s)
			}
			out[k] = map[string]any{jacksonClass: javaClassArrayList, "@items": items}
		case []any:
			items := make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, wrapListElement(item))
			}
			out[k] = map[string]any{jacksonClass: javaClassArrayList, "@items": items}
		default:
			out[k] = v
		}
	}
	return out
}

// wrapListElement 对列表元素按需附加类型信息（当前仅处理终端信息对象）。
func wrapListElement(v any) any {
	switch typed := v.(type) {
	case TerminalInfo:
		return typed
	case *TerminalInfo:
		return typed
	default:
		return v
	}
}

// unmarshalSession 解析 Jackson 多态 JSON；同时兼容不带 @class 的简化形态，
// 便于与 Go 自产会话互通。
func unmarshalSession(data []byte) (*Session, error) {
	var raw struct {
		Class      string          `json:"@class"`
		ID         string          `json:"id"`
		Type       string          `json:"type"`
		LoginType  string          `json:"loginType"`
		LoginID    json.RawMessage `json:"loginId"`
		Token      json.RawMessage `json:"token"`
		CreateTime int64           `json:"createTime"`
		Terminals  json.RawMessage `json:"terminalList"`
		DataMap    json.RawMessage `json:"dataMap"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 SaSession JSON 失败: %w", err)
	}
	s := &Session{
		ID:        raw.ID,
		Type:      raw.Type,
		LoginType: raw.LoginType,
		CreateTime: raw.CreateTime,
		DataMap:   map[string]any{},
	}
	if len(raw.LoginID) > 0 {
		var id any
		if err := json.Unmarshal(raw.LoginID, &id); err != nil {
			return nil, fmt.Errorf("解析 SaSession.loginId 失败: %w", err)
		}
		s.LoginID = id
	}
	if len(raw.Token) > 0 {
		var token any
		if err := json.Unmarshal(raw.Token, &token); err != nil {
			return nil, fmt.Errorf("解析 SaSession.token 失败: %w", err)
		}
		if sv, ok := token.(string); ok {
			s.Token = sv
		}
	}
	if len(raw.Terminals) > 0 {
		terminals, err := decodeTypedList(raw.Terminals)
		if err != nil {
			return nil, err
		}
		for _, item := range terminals {
			buf, err := json.Marshal(item)
			if err != nil {
				return nil, fmt.Errorf("序列化 terminalList 元素失败: %w", err)
			}
			var info TerminalInfo
			if err := json.Unmarshal(buf, &info); err != nil {
				return nil, fmt.Errorf("解析 terminalList 元素失败: %w", err)
			}
			s.TerminalList = append(s.TerminalList, info)
		}
	}
	if len(raw.DataMap) > 0 {
		var data map[string]json.RawMessage
		if err := json.Unmarshal(raw.DataMap, &data); err != nil {
			return nil, fmt.Errorf("解析 SaSession.dataMap 失败: %w", err)
		}
		delete(data, jacksonClass)
		for k, v := range data {
			value, err := decodeDataValue(v)
			if err != nil {
				return nil, fmt.Errorf("解析 dataMap.%s 失败: %w", k, err)
			}
			s.DataMap[k] = value
		}
	}
	return s, nil
}

// decodeTypedList 解析 Jackson 集合包装：{"@class":"java.util.ArrayList","@items":[...]}。
// 兼容直接数组形态。
func decodeTypedList(raw json.RawMessage) ([]any, error) {
	trimmed := trimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var list []any
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, fmt.Errorf("解析集合失败: %w", err)
		}
		return list, nil
	}
	var wrapper struct {
		Items []any `json:"@items"`
	}
	if err := json.Unmarshal(trimmed, &wrapper); err != nil {
		return nil, fmt.Errorf("解析 Jackson 集合包装失败: %w", err)
	}
	return wrapper.Items, nil
}

// decodeDataValue 解析 dataMap 的值：集合带 @items，标量直接返回。
func decodeDataValue(raw json.RawMessage) (any, error) {
	trimmed := trimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	switch trimmed[0] {
	case '{':
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &wrapper); err != nil {
			return nil, err
		}
		if items, ok := wrapper["@items"]; ok {
			list, err := decodeTypedList(items)
			if err != nil {
				return nil, err
			}
			out := make([]string, 0, len(list))
			for _, item := range list {
				if sv, ok := item.(string); ok {
					out = append(out, sv)
					continue
				}
				// 非字符串元素按原样保留（业务扩展场景）。
				buf, err := json.Marshal(item)
				if err != nil {
					return nil, err
				}
				out = append(out, string(buf))
			}
			return out, nil
		}
		delete(wrapper, jacksonClass)
		out := make(map[string]any, len(wrapper))
		for k, v := range wrapper {
			value, err := decodeDataValue(v)
			if err != nil {
				return nil, err
			}
			out[k] = value
		}
		return out, nil
	case '[':
		return decodeTypedList(trimmed)
	default:
		var v any
		if err := json.Unmarshal(trimmed, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
}

func trimSpace(raw []byte) []byte {
	start := 0
	for start < len(raw) {
		switch raw[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			goto tail
		}
	}
tail:
	end := len(raw)
	for end > start {
		switch raw[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			goto done
		}
	}
done:
	return raw[start:end]
}
