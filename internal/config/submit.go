package config

import (
	"encoding/json"
	"strconv"
	"strings"
)

// FormatNumber prints 50 as "50" and 0.5 as "0.5".
func FormatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Expand substitutes {id} and {_active} in a single pass, so a value that
// itself contains "{other}" is never substituted again.
func Expand(tmpl, active string, values map[string]string) string {
	pairs := make([]string, 0, 2*len(values)+2)
	pairs = append(pairs, "{_active}", active)
	for id, v := range values {
		pairs = append(pairs, "{"+id+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}

// BuildJSON renders the submitted values as a JSON object. multi holds the
// items that submit several values (they become arrays); _active is always
// included.
func BuildJSON(items []Item, values map[string]string, multi map[string][]string, active string) (string, error) {
	obj := map[string]any{"_active": active}
	for _, it := range items {
		if it.ID == "" {
			continue
		}
		if m, ok := multi[it.ID]; ok {
			if m == nil {
				m = []string{}
			}
			obj[it.ID] = m
		} else if v, ok := values[it.ID]; ok {
			obj[it.ID] = v
		}
	}
	b, err := json.Marshal(obj)
	return string(b), err
}
