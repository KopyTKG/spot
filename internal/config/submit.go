package config

import (
	"encoding/json"
	"fmt"
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

// valueVar is the variable an item's value is handed to submit commands in.
func valueVar(id string) string { return "SPOT_VALUE_" + strings.ToUpper(SanitizeID(id)) }

// SubmitEnv is env for submit.exec and submit.spawn: each item's value as
// SPOT_VALUE_<ID> and the active tiles section as SPOT_ACTIVE. A command that
// reads these, quoted, is safe whatever a value contains, unlike {id}, which is
// pasted into the command. Variables of these names inherited from the caller are
// removed, so that an empty item cannot take someone else's value.
func SubmitEnv(env []string, active string, values map[string]string) []string {
	out := make([]string, 0, len(env)+len(values)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "SPOT_VALUE_") && !strings.HasPrefix(kv, "SPOT_ACTIVE=") {
			out = append(out, kv)
		}
	}
	for id, v := range values {
		if id != "" && !strings.ContainsRune(v, 0) {
			out = append(out, valueVar(id)+"="+v)
		}
	}
	return append(out, "SPOT_ACTIVE="+active)
}

// checkValueVars rejects ids that would share a variable.
func (c *Config) checkValueVars() error {
	if c.Submit.Exec == "" {
		return nil
	}
	seen := map[string]string{}
	for _, it := range c.Items {
		if it.ID == "" {
			continue
		}
		v := valueVar(it.ID)
		if other, ok := seen[v]; ok && other != it.ID {
			return fmt.Errorf("items %q and %q would both be %s in submit.exec: rename one", other, it.ID, v)
		}
		seen[v] = it.ID
	}
	return nil
}
