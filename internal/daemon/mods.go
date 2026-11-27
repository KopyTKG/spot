package daemon

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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

// modRequest answers everything about mods but watching.
func modRequest(req protocol.Request) protocol.Response {
	fail := func(err error) protocol.Response {
		return protocol.Response{Code: protocol.ExitError, Stderr: "spot: " + approval.Visible(err.Error()) + "\n"}
	}
	answer := func(v any) protocol.Response {
		b, err := json.Marshal(v)
		if err != nil {
			return fail(err)
		}
		return protocol.Response{Result: b}
	}
	// Asking how things are, or to start anew, must not set anything up.
	switch req.Op {
	case protocol.OpMods:
		if m := running(); m != nil {
			return answer(m.Statuses())
		}
		return answer([]modd.Status{})
	case protocol.OpReload:
		if m := running(); m != nil {
			m.Reload(req.Name)
		}
		return protocol.Response{}
	}
	m, err := manager()
	if err != nil {
		return fail(err)
	}
	if req.Op == protocol.OpParse {
		result, err := m.Parse(req.Name, []byte(req.Stdin))
		if err != nil {
			return fail(err)
		}
		return protocol.Response{Result: result}
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

func running() *modd.Manager {
	mods.Lock()
	defer mods.Unlock()
	return mods.m
}

// watchPatience is how long one update may take to be read.
var watchPatience = 10 * time.Second

// send writes one update of a watch. A caller that does not read is dropped.
func send(conn net.Conn, reply func(protocol.Response) error, r protocol.Response) error {
	conn.SetWriteDeadline(time.Now().Add(watchPatience))
	return reply(r)
}

// watch sends a value now and on every change, until the caller goes.
func watch(conn net.Conn, req protocol.Request, reply func(protocol.Response) error) {
	m, err := manager()
	var w *modd.Watcher
	if err == nil {
		w, err = m.Watch(req.Name)
	}
	if err != nil {
		// Asking again would not help: the caller gives up.
		_ = reply(protocol.Response{Code: protocol.ExitUsage, Stderr: "spot: " + approval.Visible(err.Error()) + "\n"})
		return
	}
	defer m.Unwatch(w)
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, conn)
		close(gone)
	}()
	for {
		select {
		case <-gone:
			return
		case u, ok := <-w.C:
			if !ok {
				return
			}
			r := protocol.Response{Result: u.Result}
			if u.Warning != "" {
				r.Stderr = "spot: " + approval.Visible(u.Warning) + "\n"
			}
			if u.Err != "" {
				r.Code, r.Stderr = protocol.ExitError, "spot: "+approval.Visible(u.Err)+"\n"
			}
			if send(conn, reply, r) != nil {
				return
			}
		}
	}
}

// dialogMods is how the daemon's own dialogs reach mods.
type dialogMods struct{}

func (dialogMods) Get(full string) (json.RawMessage, string, error) {
	m, err := manager()
	if err != nil {
		return nil, "", err
	}
	return m.Get(full)
}

func (dialogMods) Call(full string, args []string) error {
	m, err := manager()
	if err != nil {
		return err
	}
	_, err = m.Call(full, args)
	return err
}

func (dialogMods) Parse(parser string, input []byte) (json.RawMessage, error) {
	m, err := manager()
	if err != nil {
		return nil, err
	}
	return m.Parse(parser, input)
}
