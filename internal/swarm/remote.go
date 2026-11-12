package swarm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// dockerCmd is how to run docker on h: as root, without a password prompt.
// Every docker command sshwarm runs, the probe's included, starts with it.
func dockerCmd(Host) string { return "sudo -n docker" }

// UnitLogs and StackLogs are the remote commands behind the logs view, run with LogCmd.
func UnitLogs(_ Host, unit string) string {
	return "journalctl -u " + quote(unit) + " -n 300 -o short-iso --no-pager"
}

func StackLogs(h Host, stack string) string {
	return dockerCmd(h) + " compose -p " + quote(stack) + " logs --tail 300 --no-color"
}

// LogCmd adds -f to follow; otherwise the command stops on its own on the host.
// When following, the terminal hangs it up when we leave.
func LogCmd(cmd string, follow bool) string {
	if follow {
		return cmd + " -f"
	}
	return "timeout 30 " + cmd
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// StreamLines runs remote on h and sends its output, stdout and stderr, line by line,
// without control characters. The channel closes when the command ends or ctx is
// cancelled. tty: see sshCmd.
func StreamLines(ctx context.Context, h Host, remote string, tty bool) <-chan string {
	cmd := sshCmd(ctx, h, true, tty, remote)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	go func() {
		if err := cmd.Run(); err != nil && ctx.Err() == nil && !errors.Is(err, exec.ErrWaitDelay) {
			fmt.Fprintf(pw, "[%v]\n", err)
		}
		pw.Close()
	}()

	ch := make(chan string, 256)
	go func() {
		defer close(ch)
		defer pr.Close()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case ch <- clean(sc.Text()):
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}
