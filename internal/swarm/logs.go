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

// unitLogs and stackLogs are the remote commands behind the logs view, run with logCmd.
func unitLogs(unit string) string {
	return "journalctl -u " + quote(unit) + " -n 300 -o short-iso --no-pager"
}

func stackLogs(stack string) string {
	return "sudo -n docker compose -p " + quote(stack) + " logs --tail 300 --no-color"
}

// logCmd adds -f to follow; otherwise the command stops on its own on the host.
// When following, the terminal hangs it up when we leave.
func logCmd(cmd string, follow bool) string {
	if follow {
		return cmd + " -f"
	}
	return "timeout 30 " + cmd
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// streamLines runs remote on h and sends its output, stdout and stderr, line by line,
// without control characters. The channel closes when the command ends or ctx is
// cancelled. tty: see sshCmd.
func streamLines(ctx context.Context, h Host, remote string, tty bool) <-chan string {
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
