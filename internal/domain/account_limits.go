package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type AccountConfig struct {
	ProjectsCountLimit int      `json:"projects_limit"`
	ProjectSizeLimit   ByteSize `json:"project_size_limit"`
	StorageLimit       ByteSize `json:"storage_limit"`
}

func parseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	factor := 1
	if strings.HasSuffix(value, "M") {
		factor = 1024 * 1024
	} else if strings.HasSuffix(value, "G") {
		factor = 1024 * 1024 * 1024
	}
	num, err := strconv.Atoi(strings.TrimRight(value, "MGB"))
	if err != nil {
		return -1, fmt.Errorf("Invalid byte size: %s", value)
	}
	return int64(num * factor), nil
}

type ByteSize int64

// Satisfy the flag package Value interface.
func (b *ByteSize) Set(s string) error {
	bs, err := parseByteSize(s)
	if err != nil {
		return err
	}
	*b = ByteSize(bs)
	return nil
}

func (s *ByteSize) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*s = ByteSize(value)
		return nil
	case string:
		return s.Set(value)
	default:
		return errors.New("invalid byte size")
	}
}

func (c *AccountConfig) HasStorageLimit() bool {
	return c.StorageLimit > -1
}

func (c *AccountConfig) HasProjectSizeLimit() bool {
	return c.ProjectSizeLimit > -1
}

func (c *AccountConfig) CheckStorageLimit(size int64) bool {
	return c.StorageLimit == -1 || size <= int64(c.StorageLimit)
}
func (c *AccountConfig) CheckProjectSizeLimit(size int64) bool {
	return c.ProjectSizeLimit == -1 || size <= int64(c.ProjectSizeLimit)
}

func (c *AccountConfig) CheckProjectsLimit(count int) bool {
	return c.ProjectsCountLimit == -1 || count <= c.ProjectsCountLimit
}
