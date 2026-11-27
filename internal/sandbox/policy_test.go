package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWhatIsNeverGranted(t *testing.T) {
	d := t.TempDir()
	for _, sub := range []string{"run", "config/spot", "data"} {
		os.MkdirAll(filepath.Join(d, sub), 0o700)
	}
	pr := Protected{Sockets: []string{d + "/other/spot.sock", d + "/run/spot.sock"}, NoWrite: []string{d + "/config/spot"}}
	os.Symlink(d+"/config", d+"/data/link")
	for name, p := range map[string]Policy{
		"everything":                  {Read: []string{"/"}},
		"all of /proc":                {Read: []string{"/proc"}},
		"all of /sys":                 {Read: []string{"/sys"}},
		"a device":                    {Read: []string{"/dev/input"}},
		"a process's files":           {Read: []string{"/proc/1/environ"}},
		"its own process":             {Read: []string{"/proc/self"}},
		"writing under /sys":          {Write: []string{"/sys/class/backlight"}},
		"writing the cgroups":         {Write: []string{"/sys/fs/cgroup/user.slice"}},
		"the socket":                  {Sockets: []string{d + "/run/spot.sock"}},
		"the socket's folder":         {Sockets: []string{d + "/run"}},
		"reading the socket's folder": {Read: []string{d + "/run"}},
		"writing spot's config":       {Write: []string{d + "/config/spot/mods"}},
		"writing a folder above it":   {Write: []string{d + "/config"}},
		"through a symlink":           {Write: []string{d + "/data/link"}},
		"a relative path":             {Read: []string{"data"}},
		"an unknown netlink family":   {Netlink: []string{"audit"}},
	} {
		if err := p.Check(pr); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	fine := Policy{
		Read:    []string{d + "/config/spot", d + "/data", "/proc/net", "/sys/class/power_supply", "/proc/meminfo"},
		Write:   []string{d + "/data"},
		Netlink: []string{"diag", "route"}, Ports: []uint16{443},
	}
	os.Remove(d + "/data/link")
	if err := fine.Check(pr); err != nil {
		t.Errorf("an ordinary policy: %v", err)
	}
}

func TestAHostWithoutProgramsGetsOnlyItsFile(t *testing.T) {
	rules, err := Policy{Self: "/opt/host"}.Rules()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, r := range rules {
		paths = append(paths, r.Path)
	}
	if !slices.Equal(paths, []string{"/opt/host"}) {
		t.Errorf("granted %q", paths)
	}
}

func TestScriptsGetTheirInterpreter(t *testing.T) {
	d := t.TempDir()
	for body, want := range map[string][]string{
		"#!/usr/bin/sh\n":                {"/usr/bin/sh"},
		"#! /usr/bin/env python3\n":      {"/usr/bin/env", "/usr/bin/python3"},
		"#!/usr/bin/env -S /opt/py -I\n": {"/usr/bin/env", "/opt/py"},
		"\x7fELF":                        nil,
	} {
		p := filepath.Join(d, "p")
		os.WriteFile(p, []byte(body), 0o700)
		if got, err := interpreters(p); !slices.Equal(got, want) || err != nil {
			t.Errorf("%q: %q, %v; want %q", body, got, err, want)
		}
	}
}
