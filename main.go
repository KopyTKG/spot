package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// version is set at build time: -ldflags "-X main.version=0.2.0".
var version = "dev"

const usage = `usage: spot [flags] <config>
       spot daemon
       spot --dismiss | --quit | --status

spot renders a TOML-described dialog as a Wayland layer-shell overlay.
<config> is a path to a .toml file, or a bare name looked up as
$XDG_CONFIG_HOME/spot/<name>.toml (default ~/.config/spot).

Dialogs are shown by a background daemon (started on first use, or with
"spot daemon"), so consecutive dialogs reuse one window instead of opening a
new one each time. Without a usable daemon spot shows the dialog itself.

flags:
  -h, --help       show this help
  -v, --version    print the version
  --chain          another dialog follows this one: keep the window up for it
  --last           nothing follows: release the window immediately
  --back           show a Back control (SPOT_BACK=<cmd> does too, and runs <cmd>);
                   Back ends the dialog with status 4
  --sub            open on top of the dialog that is showing; it returns when
                   this one closes (without it, the request waits its turn)
  --oneshot        do not use the daemon (also: SPOT_NO_DAEMON=1)
  --dismiss        cancel every dialog the daemon is showing or holding, and
                   release the window
  --quit           stop the daemon once it is idle
  --status         report whether the daemon is running

exit status:
  0  submitted
  1  cancelled (Esc, click outside, --dismiss)
  2  usage error
  3  config or runtime error
     (with on_error = "exit", the failed command's own status)
  4  Back (the Back control, Backspace on an empty search, or Alt+Left)

config reference: https://github.com/KopyTKG/spot#config-reference
`

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "spot: "+format+"\n", args...)
	os.Exit(exitError)
}

func usageError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "spot: "+format+"\n\n%s", append(args, usage)...)
	os.Exit(exitUsage)
}

func main() {
	var (
		arg                             string
		chain, last, sub, back, oneshot bool
		daemon, dismiss, quit, stat     bool
	)
	for _, a := range os.Args[1:] {
		switch {
		case a == "-h" || a == "--help":
			fmt.Print(usage)
			return
		case a == "-v" || a == "--version":
			fmt.Println("spot", version)
			return
		case a == "--chain":
			chain = true
		case a == "--last":
			last = true
		case a == "--sub":
			sub = true
		case a == "--back":
			back = true
		case a == "--oneshot":
			oneshot = true
		case a == "--dismiss":
			dismiss = true
		case a == "--quit":
			quit = true
		case a == "--status":
			stat = true
		case a == "daemon" && arg == "":
			daemon = true
		case strings.HasPrefix(a, "-") && a != "-":
			usageError("unknown flag %q", a)
		case arg != "" || daemon:
			usageError("only one config file is accepted")
		default:
			arg = a
		}
	}
	if chain && last {
		usageError("--chain and --last contradict each other")
	}

	switch {
	case daemon:
		os.Exit(runDaemon())
	case dismiss:
		os.Exit(control(opDismiss))
	case quit:
		os.Exit(control(opQuit))
	case stat:
		os.Exit(control(opPing))
	case arg == "":
		fmt.Fprint(os.Stderr, usage)
		os.Exit(exitUsage)
	}

	cfgPath, err := filepath.Abs(resolveConfigPath(arg))
	if err != nil {
		fatal("%v", err)
	}
	req := request{
		Op: opShow, Version: version, Config: cfgPath, Env: os.Environ(),
		Chain: chain, Last: last, Sub: sub, Back: back,
	}
	req.Dir, _ = os.Getwd()

	// Parse here too: errors should surface before any window or daemon, and
	// we need to know whether the dialog wants stdin.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		fatal("%v", err)
	}
	cfg, err := parseConfig(data, os.LookupEnv)
	if err != nil {
		fatal("%s: %v", cfgPath, err)
	}
	for _, it := range cfg.Items {
		if it.Type == "list" && it.Stdin {
			var b strings.Builder
			sc := bufio.NewScanner(os.Stdin)
			sc.Buffer(make([]byte, 1024*1024), 1024*1024)
			for sc.Scan() {
				b.WriteString(sc.Text())
				b.WriteByte('\n')
			}
			req.Stdin = b.String()
			break
		}
	}

	var resp response
	if oneshot || os.Getenv("SPOT_NO_DAEMON") != "" || os.Getenv("WAYLAND_DISPLAY") == "" {
		resp = runOneshot(req)
	} else if r, ok := viaDaemon(req); ok {
		resp = r
	} else {
		resp = runOneshot(req)
	}

	os.Stdout.WriteString(resp.Stdout)
	os.Stderr.WriteString(resp.Stderr)
	os.Exit(resp.Code)
}

// runOneshot shows the dialog in a window of its own and exits with the answer.
func runOneshot(req request) response {
	j, err := buildJob(req)
	if err != nil {
		return response{Code: exitError, Stderr: fmt.Sprintf("spot: %v\n", err)}
	}
	// Per-invocation app ID so concurrent `spot` calls don't dedupe.
	base := strings.TrimSuffix(filepath.Base(req.Config), filepath.Ext(req.Config))
	app := gtk.NewApplication(appID(base, os.Getpid()), gio.ApplicationFlagsNone)
	sh := newShell(app, true)
	app.ConnectActivate(func() {
		sh.init()
		sh.sched.enqueue(j)
	})
	app.Run(nil)

	select {
	case r := <-j.reply:
		return r
	default:
		return response{Code: exitError}
	}
}
