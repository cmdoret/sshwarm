// Package remote runs commands on hosts over ssh: to their end, as a stream of
// lines, or with the user's terminal.
package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/cmdoret/sshwarm/internal/inventory"
)

// Command is ssh running cmd on h, or with cmd "", a login shell. It never prompts,
// and gives up on dead hosts and dead connections quickly. tty gives the remote
// command a terminal, so that it's hung up when ssh ends (needed to stop a followed
// log).
func Command(ctx context.Context, h inventory.Host, tty bool, cmd string) *exec.Cmd {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=10",
		"-o", "ServerAliveCountMax=2",
	}
	if tty {
		args = append(args, "-tt", "-q")
	}

	args = slices.Concat(args, h.Opts, []string{"--", h.Address})
	if cmd != "" {
		args = append(args, cmd)
	}

	c := exec.CommandContext(ctx, "ssh", args...)
	// When ~/.ssh/config shares connections (ControlPersist), the background master
	// keeps ssh's output open: don't wait for it once ssh itself has exited. Wait then
	// returns exec.ErrWaitDelay.
	c.WaitDelay = time.Second

	return c
}

// Run runs cmd on h to its end, without a time limit, and returns its output, stdout
// and stderr, without control characters. Without a terminal, cmd goes on on the
// host if ssh is killed.
func Run(ctx context.Context, h inventory.Host, cmd string) (string, error) {
	out, err := Command(ctx, h, false, cmd).CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) { // see Command: ssh itself is done
		err = nil
	}

	return Clean(string(out)), err
}

// Interactive is ssh running cmd on h with the user's terminal, or with cmd "", a
// login shell. The caller hands it the terminal.
func Interactive(ctx context.Context, h inventory.Host, cmd string) *exec.Cmd {
	return Command(ctx, h, true, cmd)
}

// Stream runs cmd on h, with a terminal if tty, and sends its output, stdout and
// stderr, line by line, without control characters; a failure, of cmd or of
// reading it, ends it with a line saying so. The channel closes when the command ends or ctx is cancelled.
func Stream(ctx context.Context, h inventory.Host, cmd string, tty bool) <-chan string {
	c := Command(ctx, h, tty, cmd)
	pr, pw := io.Pipe()
	c.Stdout, c.Stderr = pw, pw

	go func() {
		if err := c.Run(); err != nil && ctx.Err() == nil && !errors.Is(err, exec.ErrWaitDelay) {
			_, _ = fmt.Fprintf(pw, "[%v]\n", err) // the reader may be gone: no one left to tell
		}

		pw.Close()
	}()

	ch := make(chan string)

	go func() {
		defer close(ch)
		defer pr.Close()

		sc := bufio.NewScanner(pr)
		sc.Buffer(nil, 1024*1024) // log lines can be long JSON

		for sc.Scan() {
			select {
			case ch <- Clean(sc.Text()):
			case <-ctx.Done():
				return
			}
		}

		if err := sc.Err(); err != nil { // a line too long: the rest is cut, say so
			select {
			case ch <- "[" + err.Error() + "]":
			case <-ctx.Done():
			}
		}
	}()

	return ch
}

// Clean makes remote text plain: it drops escape sequences, then any other control
// character but tabs and newlines.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || !unicode.IsControl(r) {
			return r
		}

		return -1
	}, ansi.Strip(s))
}
