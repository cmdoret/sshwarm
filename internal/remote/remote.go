// Package remote runs commands on hosts over ssh: to their end, as a stream of
// lines, or with the user's terminal.
package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	// When ssh shares connections (Share, or the user's ControlPersist), the master a
	// run starts goes on in the background, and may keep ssh's output open: don't wait
	// for it once ssh itself has exited. Wait then returns exec.ErrWaitDelay.
	c.WaitDelay = time.Second

	return c
}

// Share has ssh share one connection per host between the probes, logs and
// actions, rather than connect and log in for each. If the user's ssh config
// sets how it shares them or it cannot create sockets, connections are not shared.
func Share(ctx context.Context, hosts []inventory.Host) []inventory.Host {
	dir := socketDir()
	if dir == "" {
		return hosts
	}

	// A connection lasts a minute past its last use: past the next probe, at the
	// default refresh. Quoted, as a home can hold spaces.
	opts := []string{"-o", "ControlMaster=auto", "-o", `ControlPath="` + dir + `/sshwarm-%C"`, "-o", "ControlPersist=60"}
	hosts = slices.Clone(hosts)

	var wg sync.WaitGroup

	for i := range hosts {
		wg.Go(func() {
			if !shares(ctx, hosts[i]) {
				hosts[i].Opts = slices.Concat(opts, hosts[i].Opts)
			}
		})
	}

	wg.Wait()

	return hosts
}

// socketDir is where ssh can keep the sockets of shared connections: the
// user's ~/.ssh, if it can be written to. Returns "" otherwise.
func socketDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	dir := filepath.Join(home, ".ssh")
	// too long for a MacOS socket path
	if len(dir)+len("/sshwarm-")+40+17 > 104 {
		return ""
	}

	f, err := os.CreateTemp(dir, ".sshwarm-*")
	if err != nil {
		return ""
	}

	_ = f.Close()
	_ = os.Remove(f.Name()) // a leftover empty file is harmless

	return dir
}

// shares says whether the user's ssh config already shares connections for a host.
// Returns true if unable to determine.
func shares(ctx context.Context, h inventory.Host) bool {
	out, err := exec.CommandContext(ctx, "ssh", slices.Concat([]string{"-G"}, h.Opts, []string{"--", h.Address})...).Output()
	if err != nil {
		return true
	}

	for line := range strings.Lines(string(out)) {
		k, v, _ := strings.Cut(strings.TrimSpace(line), " ")
		if k == "controlpath" || k == "controlmaster" && v != "false" {
			return true
		}
	}

	return false
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
