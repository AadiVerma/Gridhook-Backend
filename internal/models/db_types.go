package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

func scanString(src any) (string, error) {
	switch s := src.(type) {
	case []byte:
		return string(s), nil
	case string:
		return s, nil
	default:
		return "", fmt.Errorf("models: unsupported scan source %T", src)
	}
}

func (e *UserRole) Scan(src any) error {
	s, err := scanString(src)
	*e = UserRole(s)
	return err
}
func (e UserRole) Value() (driver.Value, error) { return string(e), nil }

func (e *UserStatus) Scan(src any) error {
	s, err := scanString(src)
	*e = UserStatus(s)
	return err
}
func (e UserStatus) Value() (driver.Value, error) { return string(e), nil }

func (e *ConnectorStatus) Scan(src any) error {
	s, err := scanString(src)
	*e = ConnectorStatus(s)
	return err
}
func (e ConnectorStatus) Value() (driver.Value, error) { return string(e), nil }

func (e *EngineType) Scan(src any) error {
	s, err := scanString(src)
	*e = EngineType(s)
	return err
}
func (e EngineType) Value() (driver.Value, error) { return string(e), nil }

func (e *AuthType) Scan(src any) error {
	s, err := scanString(src)
	*e = AuthType(s)
	return err
}
func (e AuthType) Value() (driver.Value, error) { return string(e), nil }

func (e *ToolGroupKind) Scan(src any) error {
	s, err := scanString(src)
	*e = ToolGroupKind(s)
	return err
}
func (e ToolGroupKind) Value() (driver.Value, error) { return string(e), nil }

func (e *HTTPMethod) Scan(src any) error {
	s, err := scanString(src)
	*e = HTTPMethod(s)
	return err
}
func (e HTTPMethod) Value() (driver.Value, error) { return string(e), nil }

func (e *ToolStatus) Scan(src any) error {
	s, err := scanString(src)
	*e = ToolStatus(s)
	return err
}
func (e ToolStatus) Value() (driver.Value, error) { return string(e), nil }

func (e *MCPServerStatus) Scan(src any) error {
	s, err := scanString(src)
	*e = MCPServerStatus(s)
	return err
}
func (e MCPServerStatus) Value() (driver.Value, error) { return string(e), nil }

func (e *InvocationStatus) Scan(src any) error {
	s, err := scanString(src)
	*e = InvocationStatus(s)
	return err
}
func (e InvocationStatus) Value() (driver.Value, error) { return string(e), nil }

type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("models: unsupported scan source %T for JSONMap", src)
	}
	return json.Unmarshal(data, m)
}
