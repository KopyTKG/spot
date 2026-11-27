package daemon

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"gitlab.com/KopyTKG/spot/internal/approval"
	"gitlab.com/KopyTKG/spot/internal/modd"
	"gitlab.com/KopyTKG/spot/internal/modhost"
	"gitlab.com/KopyTKG/spot/internal/protocol"
)

// mods is made at the first request about a mod: a daemon nobody asks does
// nothing it did not do before.
var mods struct {
	sync.Mutex
	m *modd.Manager
}

// ModStore finds the user's mods. A variable so that a test build can point it elsewhere.
var ModStore = approval.Default

func manager() (*modd.Manager, error) {
	mods.Lock()
	defer mods.Unlock()
	if mods.m != nil {
		return mods.m, nil
	}
	store, err := ModStore()
	if err != nil {
		return nil, err
	}
	host, err := modhost.HostPath()
	if err != nil {
		return nil, err
	}
	m, err := modd.New(store, host, "/run/user/"+strconv.Itoa(os.Getuid()))
	if err != nil {
		return nil, err
	}
	modd.CleanStale()
	mods.m = m
	return m, nil
}

func closeMods() {
	mods.Lock()
	defer mods.Unlock()
	if mods.m != nil {
		mods.m.Close()
		mods.m = nil
	}
}

// fromAMod reports whether the process pid is a mod host or was started by one:
// the daemon takes nothing from them (S21). The sandbox keeps them from the
// socket already.
func fromAMod(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
	return err == nil && strings.Contains(string(b), "/spot-mod-")
}

// modRequest answers `spot get` and `spot call`.
func modRequest(req protocol.Request) protocol.Response {
	fail := func(err error) protocol.Response {
		return protocol.Response{Code: protocol.ExitError, Stderr: "spot: " + approval.Visible(err.Error()) + "\n"}
	}
	m, err := manager()
	if err != nil {
		return fail(err)
	}
	if req.Op == protocol.OpCall {
		result, err := m.Call(req.Name, req.Args)
		if err != nil {
			return fail(err)
		}
		return protocol.Response{Result: result}
	}
	result, warning, err := m.Get(req.Name)
	if err != nil {
		return fail(err)
	}
	r := protocol.Response{Result: result}
	if warning != "" {
		r.Stderr = "spot: " + approval.Visible(warning) + "\n"
	}
	return r
}
