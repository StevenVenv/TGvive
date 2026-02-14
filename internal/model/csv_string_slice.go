package model

import (
	"bytes"
	"encoding/json"
	"strings"
)

// CSVStringSlice stores comma-separated values in DB, but marshals/unmarshals as JSON string array.
type CSVStringSlice string

func (s CSVStringSlice) Strings() []string {
	raw := strings.TrimSpace(string(s))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s CSVStringSlice) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Strings())
}

func (s *CSVStringSlice) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*s = ""
		return nil
	}

	if b[0] == '[' {
		var items []string
		if err := json.Unmarshal(b, &items); err != nil {
			return err
		}
		clean := make([]string, 0, len(items))
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			clean = append(clean, item)
		}
		*s = CSVStringSlice(strings.Join(clean, ","))
		return nil
	}

	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*s = ""
		return nil
	}

	parts := strings.Split(raw, ",")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		clean = append(clean, part)
	}
	*s = CSVStringSlice(strings.Join(clean, ","))
	return nil
}
