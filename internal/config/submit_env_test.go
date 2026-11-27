package config

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestSubmitValuesCannotBecomeCommands(t *testing.T) {
	nasty := "$(echo INJECTED); `echo INJECTED` \"x\" 'y' -- *"
	env := SubmitEnv([]string{"PATH=/usr/bin", "SPOT_VALUE_OTHER=stale", "SPOT_ACTIVE=stale"}, "sec", map[string]string{"my-host": nasty})
	c := exec.Command("sh", "-c", `printf '%s|%s|%s' "$SPOT_VALUE_MY_HOST" "${SPOT_VALUE_OTHER-unset}" "$SPOT_ACTIVE"`)
	c.Env = env
	out, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := nasty + "|unset|sec"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if slices.ContainsFunc(env, func(kv string) bool { return strings.HasSuffix(kv, "=stale") }) {
		t.Errorf("an inherited value was kept: %q", env)
	}
}

func TestIdsSharingAVariableAreRefused(t *testing.T) {
	_, err := ParseConfig([]byte("[[items]]\ntype='field'\nid='a-b'\n[[items]]\ntype='field'\nid='a_b'\n[submit]\nexec='true'\n"), noEnv)
	if err == nil || !strings.Contains(err.Error(), "SPOT_VALUE_A_B") {
		t.Errorf("err = %v", err)
	}
}

func TestPasswordDialogsAreRecognised(t *testing.T) {
	c, err := ParseConfig([]byte("[[items]]\ntype='password'\nid='p'\n"), noEnv)
	if err != nil || !c.HasPassword() {
		t.Errorf("HasPassword = %v, %v", c != nil && c.HasPassword(), err)
	}
}
