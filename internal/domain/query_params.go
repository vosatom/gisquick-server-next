package domain

import (
	"encoding/json"
	"fmt"
)

type QueryParams map[string]json.RawMessage

func (p QueryParams) Value(key string) (ParamValue, bool) {
	var val ParamValue
	value, ok := p[key]
	if ok {
		if err := json.Unmarshal(value, &val); err != nil {
			ok = false
		}
	}
	return val, ok
}

func (p QueryParams) String(key string) string {
	val, _ := p.Value(key)
	return val.String()
}

func (p QueryParams) StringArray(key string) []string {
	val, _ := p.Value(key)
	return val.StringArray()
}

/*
type ParamValue struct {
	values []string
}

func (p *ParamValue) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("no bytes to unmarshal")
	}
	switch b[0] {
	case '"':
		var value string
		if err := json.Unmarshal(b, &value); err != nil {
			return err
		}
		p.values = []string{value}
	case '[':
		return json.Unmarshal(b, &p.values)
	}
	return nil
}

func (p *ParamValue) String() string {
	if len(p.values) > 0 {
		return p.values[0]
	}
	return ""
}

func (p *ParamValue) StringArray() []string {
	return p.values
}
*/
type ParamValue []string

func (p *ParamValue) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("no bytes to unmarshal")
	}
	switch b[0] {
	case '"':
		var value string
		if err := json.Unmarshal(b, &value); err != nil {
			return err
		}
		*p = []string{value}
	case '[':
		var value []string
		if err := json.Unmarshal(b, &value); err != nil {
			return err
		}
		*p = value
	}
	return nil
}

func (p *ParamValue) String() string {
	if len(*p) > 0 {
		return (*p)[0]
	}
	return ""
}

func (p *ParamValue) StringArray() []string {
	return *p
}
