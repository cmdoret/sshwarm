package swarm

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
)

// docker runs docker on a machine, as root without a password prompt. Every
// docker command sshwarm runs starts with it, the probe's included.
const docker = "sudo -n docker"

// sshCmd runs remote on h. It never prompts, and gives up on dead hosts and dead
// connections quickly. tty gives the remote command a terminal, so that it's hung
// up when ssh ends (needed to stop a followed log).
func sshCmd(ctx context.Context, h Host, tty bool, remote string) *exec.Cmd {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=10",
		"-o", "ServerAliveCountMax=2",
	}
	if tty {
		args = append(args, "-tt", "-q")
	}

	cmd := exec.CommandContext(ctx, "ssh", slices.Concat(args, h.Opts, []string{"--", h.Address, remote})...)
	// When ~/.ssh/config shares connections (ControlPersist), the background master
	// keeps ssh's output open: don't wait for it once ssh itself has exited. Wait then
	// returns exec.ErrWaitDelay.
	cmd.WaitDelay = time.Second

	return cmd
}

// UnitLogs and StackLogs are the remote commands behind the logs view, run with LogCmd.
func UnitLogs(unit string) string {
	return "journalctl -u " + quote(unit) + " -n 300 -o short-iso --no-pager"
}

func StackLogs(stack string) string {
	return docker + " compose -p " + quote(stack) + " logs --tail 300 --no-color"
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
	cmd := sshCmd(ctx, h, tty, remote)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw

	go func() {
		if err := cmd.Run(); err != nil && ctx.Err() == nil && !errors.Is(err, exec.ErrWaitDelay) {
			fmt.Fprintf(pw, "[%v]\n", err)
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
			case ch <- clean(sc.Text()):
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}
