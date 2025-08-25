package cache

import (
	"encoding/json"
	"fmt"
)

// Serializer 序列化器接口
type Serializer interface {
	// Marshal 序列化
	Marshal(v interface{}) ([]byte, error)

	// Unmarshal 反序列化
	Unmarshal(data []byte, v interface{}) error

	// ContentType 内容类型
	ContentType() string
}

// JSONSerializer JSON序列化器
type JSONSerializer struct{}

// Marshal 序列化为JSON
func (s *JSONSerializer) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal 从JSON反序列化
func (s *JSONSerializer) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// ContentType 返回内容类型
func (s *JSONSerializer) ContentType() string {
	return "application/json"
}

// StringSerializer 字符串序列化器（用于简单的字符串类型）
type StringSerializer struct{}

// Marshal 序列化字符串
func (s *StringSerializer) Marshal(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case string:
		return []byte(val), nil
	case []byte:
		return val, nil
	default:
		return nil, fmt.Errorf("StringSerializer: unsupported type %T", v)
	}
}

// Unmarshal 反序列化字符串
func (s *StringSerializer) Unmarshal(data []byte, v interface{}) error {
	switch ptr := v.(type) {
	case *string:
		*ptr = string(data)
		return nil
	case *[]byte:
		*ptr = data
		return nil
	default:
		return fmt.Errorf("StringSerializer: unsupported type %T", v)
	}
}

// ContentType 返回内容类型
func (s *StringSerializer) ContentType() string {
	return "text/plain"
}
