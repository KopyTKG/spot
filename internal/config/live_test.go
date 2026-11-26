package config

import (
	"strings"
	"testing"
)

func TestLiveControlConfigErrors(t *testing.T) {
	for toml, want := range map[string]string{
		"type='switch'":                                             "needs an id",
		"type='switch'\nid='a'\nlook='dial'":                        "look",
		"type='segmented'\nid='a'":                                  "options",
		"type='segmented'\nid='a'\noptions=['x']\nlabels=['y','z']": "labels",
		"type='button'\nlabel='a'":                                  "exec",
		"type='field'\nid='a'\nstate='x'":                           "state and on_change",
		"type='list'\nid='a'\non_change='x'":                        "state and on_change",
		"type='list'\nid='a'\nswitches=true\nmulti=true":            "switches",
		"type='field'\nid='a'\nexec='x'":                            "button",
		"type='field'\nid='a'\nlook='button'":                       "switch",
		"type='scale'\nid='a'\ntimeout=-1":                          "timeout",
	} {
		_, err := ParseConfig([]byte("[[items]]\n"+toml+"\n"), noEnv)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want it to mention %q", toml, err, want)
		}
	}
}

func TestParseSwitch(t *testing.T) {
	yes := "enabled"
	it := &Item{On: &yes}
	for out, want := range map[string][2]bool{
		"on\n": {true, true}, "OFF": {false, true}, "enabled": {true, true}, "false": {false, true},
		"checking...\nyes\n": {true, true}, "": {false, false}, "maybe": {false, false},
	} {
		on, ok := it.ParseSwitch(out)
		if on != want[0] || ok != want[1] {
			t.Errorf("ParseSwitch(%q) = %v, %v", out, on, ok)
		}
	}
}
