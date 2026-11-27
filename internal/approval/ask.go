package approval

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Wait is how long the prompt holds back, so that Enter hit in passing, or
// text pasted along with the command, approves nothing.
const Wait = 2 * time.Second

// Terminal is the terminal itself, not whatever stdin and stdout are: an
// approval cannot be piped in, and what is asked cannot be redirected away.
func Terminal() (*os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil {
		if _, err = unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS); err != nil {
			tty.Close()
		}
	}
	if err != nil {
		return nil, errors.New("approving needs a terminal you are typing into")
	}
	return tty, nil
}

// Ask shows what must be typed and reads it, discarding whatever was typed
// before. It reports whether exactly those words came back.
func Ask(tty *os.File, items []Item, words []string, name string) (bool, error) {
	var dangers []string
	for _, i := range items {
		if i.Danger != "" {
			dangers = append(dangers, i.Kind+" "+Oneline(i.What))
		}
	}
	if len(dangers) > 0 {
		fmt.Fprintf(tty, "\nDangerous, see above: %s\n", strings.Join(dangers, "; "))
	}
	for _, c := range Combinations(items) {
		fmt.Fprintf(tty, "!! %s\n", c)
	}
	time.Sleep(Wait)
	if err := unix.IoctlSetInt(int(tty.Fd()), unix.TCFLSH, unix.TCIFLUSH); err != nil {
		return false, fmt.Errorf("discarding earlier input: %w", err)
	}
	if len(words) == 1 && words[0] == name {
		fmt.Fprintf(tty, "\nType the mod's name (%s) to approve it, or press Enter to refuse: ", name)
	} else {
		fmt.Fprintf(tty, "\nType each of these to accept it (%s), or press Enter to refuse: ", strings.Join(words, ", "))
	}
	line, err := bufio.NewReader(io.LimitReader(tty, 4096)).ReadString('\n')
	if err != nil {
		fmt.Fprintln(tty)
		return false, nil // closed or interrupted: not an approval
	}
	return Accepted(line, words), nil
}
