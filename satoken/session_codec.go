package satoken

import (
	"encoding/json"
	"fmt"
)

// TerminalInfo 对应 cn.dev33.satoken.session.SaTerminalInfo。
//
// index、tokenValue、deviceType、deviceId、extraData、createTime。
// 注意 index 由 SaSession.addTerminal 赋值（从 1 开始），Go 侧同样维护。
type TerminalInfo struct {
	// Index 终端序号，从 1 开始（Java: SaSession.addTerminal -> setIndex(++historyTerminalCount) 语义）。
	Index int `json:"index,omitempty"`
	// TokenValue 该终端对应的 token。
	TokenValue string `json:"tokenValue,omitempty"`
	// DeviceType 设备类型，Java 默认 "DEF"。
	DeviceType string `json:"deviceType,omitempty"`
	// DeviceID 设备 id，可为空。
	DeviceID string `json:"deviceId,omitempty"`
	// ExtraData 终端扩展数据。
	ExtraData map[string]any `json:"extraData,omitempty"`
	// CreateTime 毫秒时间戳。
	CreateTime int64 `json:"createTime,omitempty"`
}

// MarshalJSON 输出带 Jackson 类型信息的终端信息（@class 属性形式，字段顺序与 Java 一致）。
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
// 会话 JSON 形态（Sa-Token 1.44.0 + sa-token-jackson 约定）：
//
//	{"@class":"cn.dev33.satoken.session.SaSession",
//	 "id":"<uuid>","type":"Account-Session","loginType":"login",
//	 "loginId":"0","token":null,"historyTerminalCount":1,"createTime":1789317424024,
//	 "dataMap":["java.util.concurrent.ConcurrentHashMap",
//	            {"permissions":["java.util.ImmutableCollections$List12",["a","b"]],"username":"root"}],
//	 "terminalList":["java.util.Vector",[{"@class":"cn.dev33.satoken.session.SaTerminalInfo", ...}]]}
//
// 关键点（与初期实现不同，勿回退）：
//   - 集合/Map 用 Jackson 的 **WRAPPER_ARRAY** 形态：`["<实现类全限定名>", <值>]`，
//     不是 `{"@class":...,"@items":[...]}`；只有非 final 的自定义类（SaSession/SaTerminalInfo）
//     才用 `@class` 属性形式。
//   - dataMap 的实现类是 ConcurrentHashMap，terminalList 是 Vector。
//   - 会话含 historyTerminalCount 字段（终端历史计数，删终端不减）。
type Session struct {
	ID        string
	Type      string
	LoginType string
	LoginID   any
	Token     string
	// HistoryTerminalCount 历史终端计数；新增终端时自增。
	HistoryTerminalCount int
	// CreateTime 毫秒时间戳。
	CreateTime int64
	// TerminalList 终端列表；空时序列化为 null（Jackson 未初始化列表同样输出 null）。
	TerminalList []TerminalInfo
	// DataMap 业务数据（permissions/roles/username 等）。
	DataMap map[string]any
}

// 与
const (
	javaClassSessionMap     = "java.util.concurrent.ConcurrentHashMap"
	javaClassTerminalVector = "java.util.Vector"
	javaClassListWrapper    = "java.util.ArrayList"
)

// NewSession 构造与 Java 侧 new SaSession(id) 等价的空会话。
func NewSession(id string) *Session {
	return &Session{
		ID:           id,
		DataMap:      map[string]any{},
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

// StringList 读取 dataMap 中的字符串列表，兼容 []string、[]any 与 Jackson 包装形态。
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

// AddTerminal 追加终端并维护 index/historyTerminalCount。
func (s *Session) AddTerminal(info TerminalInfo) {
	for _, existing := range s.TerminalList {
		if existing.TokenValue == info.TokenValue && info.TokenValue != "" {
			return
		}
	}
	s.HistoryTerminalCount++
	info.Index = s.HistoryTerminalCount
	if info.DeviceType == "" {
		info.DeviceType = "DEF"
	}
	s.TerminalList = append(s.TerminalList, info)
}

// RemoveTerminal 移除指定 token 的终端（historyTerminalCount 不回退）。
func (s *Session) RemoveTerminal(tokenValue string) {
	if len(s.TerminalList) == 0 {
		return
	}
	kept := s.TerminalList[:0]
	for _, terminal := range s.TerminalList {
		if terminal.TokenValue == tokenValue {
			continue
		}
		kept = append(kept, terminal)
	}
	s.TerminalList = kept
}

// marshalSession 生成与 sa-token-jackson（Jackson default typing, NON_FINAL）逐字段对齐的 JSON。
func marshalSession(s *Session) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("session 不能为空")
	}
	type sessionAlias struct {
		ID                   string `json:"id"`
		Type                 string `json:"type,omitempty"`
		LoginType            string `json:"loginType,omitempty"`
		LoginID              any    `json:"loginId"`
		Token                any    `json:"token"`
		HistoryTerminalCount int    `json:"historyTerminalCount"`
		CreateTime           int64  `json:"createTime"`
		DataMap              any    `json:"dataMap"`
		TerminalList         any    `json:"terminalList"`
	}
	var token any
	if s.Token != "" {
		token = s.Token
	}
	terminals := any(nil)
	if len(s.TerminalList) > 0 {
		items := make([]any, 0, len(s.TerminalList))
		for _, t := range s.TerminalList {
			items = append(items, t)
		}
		// Jackson 对集合使用 WRAPPER_ARRAY：["java.util.Vector", [...]]
		terminals = []any{javaClassTerminalVector, items}
	}
	// dataMap 恒为对象。
	dataMap := any(nil)
	if s.DataMap != nil {
		wrapped := make(map[string]any, len(s.DataMap))
		for k, v := range s.DataMap {
			wrapped[k] = wrapValue(v)
		}
		dataMap = []any{javaClassSessionMap, wrapped}
	}
	payload := struct {
		Class string `json:"@class"`
		sessionAlias
	}{
		Class: javaClassSession,
		sessionAlias: sessionAlias{
			ID:                   s.ID,
			Type:                 s.Type,
			LoginType:            s.LoginType,
			LoginID:              s.LoginID,
			Token:                token,
			HistoryTerminalCount: s.HistoryTerminalCount,
			CreateTime:           s.CreateTime,
			DataMap:              dataMap,
			TerminalList:         terminals,
		},
	}
	return json.Marshal(payload)
}

// wrapValue 按 Jackson 规则包装集合值（WRAPPER_ARRAY）；标量原样保留。
func wrapValue(v any) any {
	switch typed := v.(type) {
	case []string:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return []any{javaClassListWrapper, items}
	case []any:
		return []any{javaClassListWrapper, typed}
	default:
		return v
	}
}

// unmarshalSession 解析 Jackson 多态 JSON，同时兼容早期 Go 实现的属性形态。
func unmarshalSession(data []byte) (*Session, error) {
	var raw struct {
		Class                string          `json:"@class"`
		ID                   string          `json:"id"`
		Type                 string          `json:"type"`
		LoginType            string          `json:"loginType"`
		LoginID              json.RawMessage `json:"loginId"`
		Token                json.RawMessage `json:"token"`
		HistoryTerminalCount int             `json:"historyTerminalCount"`
		CreateTime           int64           `json:"createTime"`
		Terminals            json.RawMessage `json:"terminalList"`
		DataMap              json.RawMessage `json:"dataMap"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 SaSession JSON 失败: %w", err)
	}
	s := &Session{
		ID:                   raw.ID,
		Type:                 raw.Type,
		LoginType:            raw.LoginType,
		HistoryTerminalCount: raw.HistoryTerminalCount,
		CreateTime:           raw.CreateTime,
		DataMap:              map[string]any{},
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
		items, err := decodeCollection(raw.Terminals)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
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
		if s.HistoryTerminalCount < len(s.TerminalList) {
			s.HistoryTerminalCount = len(s.TerminalList)
		}
	}
	if len(raw.DataMap) > 0 {
		value, err := decodeDataMap(raw.DataMap)
		if err != nil {
			return nil, err
		}
		s.DataMap = value
	}
	return s, nil
}

// decodeCollection 解析集合：兼容 Jackson WRAPPER_ARRAY `["impl", [...]]`、
// 早期属性包装 `{"@class":...,"@items":[...]}` 与裸数组。
func decodeCollection(raw json.RawMessage) ([]any, error) {
	trimmed := trimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		var list []any
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, fmt.Errorf("解析集合失败: %w", err)
		}
		// WRAPPER_ARRAY 形态：首元素是字符串类名时，真实数据在第二元素。
		if len(list) == 2 {
			if _, ok := list[0].(string); ok {
				if inner, ok := list[1].([]any); ok {
					return inner, nil
				}
			}
		}
		return list, nil
	case '{':
		var wrapper struct {
			Items []any `json:"@items"`
		}
		if err := json.Unmarshal(trimmed, &wrapper); err != nil {
			return nil, fmt.Errorf("解析集合包装失败: %w", err)
		}
		return wrapper.Items, nil
	default:
		return nil, fmt.Errorf("无法识别的集合形态: %s", truncateJSON(trimmed))
	}
}

// decodeDataMap 解析 dataMap：兼容 WRAPPER_ARRAY、属性包装与裸对象。
func decodeDataMap(raw json.RawMessage) (map[string]any, error) {
	trimmed := trimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return map[string]any{}, nil
	}
	if trimmed[0] == '[' {
		// WRAPPER_ARRAY：["java.util.concurrent.ConcurrentHashMap", {...}]
		var wrapper []json.RawMessage
		if err := json.Unmarshal(trimmed, &wrapper); err != nil {
			return nil, fmt.Errorf("解析 dataMap 包装失败: %w", err)
		}
		if len(wrapper) != 2 {
			return nil, fmt.Errorf("dataMap WRAPPER_ARRAY 长度异常: %d", len(wrapper))
		}
		return decodePlainMap(wrapper[1])
	}
	if trimmed[0] == '{' {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &probe); err != nil {
			return nil, fmt.Errorf("解析 dataMap 失败: %w", err)
		}
		if _, isWrapper := probe["@items"]; isWrapper {
			delete(probe, jacksonClass)
			out := make(map[string]any, len(probe))
			for k, v := range probe {
				value, err := decodeDataValue(v)
				if err != nil {
					return nil, fmt.Errorf("解析 dataMap.%s 失败: %w", k, err)
				}
				out[k] = value
			}
			return out, nil
		}
		return decodePlainMap(trimmed)
	}
	return nil, fmt.Errorf("无法识别的 dataMap 形态: %s", truncateJSON(trimmed))
}

func decodePlainMap(raw json.RawMessage) (map[string]any, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("解析 dataMap 对象失败: %w", err)
	}
	delete(probe, jacksonClass)
	out := make(map[string]any, len(probe))
	for k, v := range probe {
		value, err := decodeDataValue(v)
		if err != nil {
			return nil, fmt.Errorf("解析 dataMap.%s 失败: %w", k, err)
		}
		out[k] = value
	}
	return out, nil
}

// decodeDataValue 解析 dataMap 的值：字符串列表统一转 []string，其余保持原形态。
func decodeDataValue(raw json.RawMessage) (any, error) {
	trimmed := trimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		list, err := decodeCollection(trimmed)
		if err != nil {
			return nil, err
		}
		allStrings := true
		out := make([]string, 0, len(list))
		for _, item := range list {
			sv, ok := item.(string)
			if !ok {
				allStrings = false
				break
			}
			out = append(out, sv)
		}
		if allStrings {
			return out, nil
		}
		return list, nil
	case '{':
		// 早期 Go 实现的属性包装 {"@class":...,"@items":[...]} 也走集合解析。
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &probe); err == nil {
			if _, isWrapper := probe["@items"]; isWrapper {
				return decodeCollection(trimmed)
			}
		}
		return decodePlainMap(trimmed)
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

func truncateJSON(raw []byte) string {
	const max = 80
	if len(raw) <= max {
		return string(raw)
	}
	return string(raw[:max]) + "..."
}
