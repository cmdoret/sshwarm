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

const (
	// logTail is the history a logs view opens with.
	logTail = 300

	// oneShotTimeout stops a command that isn't followed, on the host, after a number
	// of seconds.
	oneShotTimeout = 30
)

// sshCmd runs remote on h, or with remote "", a login shell. It never prompts, and
// gives up on dead hosts and dead connections quickly. tty gives the remote command
// a terminal, so that it's hung up when ssh ends (needed to stop a followed log).
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

	args = slices.Concat(args, h.Opts, []string{"--", h.Address})
	if remote != "" {
		args = append(args, remote)
	}

	cmd := exec.CommandContext(ctx, "ssh", args...)
	// When ~/.ssh/config shares connections (ControlPersist), the background master
	// keeps ssh's output open: don't wait for it once ssh itself has exited. Wait then
	// returns exec.ErrWaitDelay.
	cmd.WaitDelay = time.Second

	return cmd
}

// UnitLogs is the log command of a systemd unit, for Stream.
func UnitLogs(unit string) string {
	return fmt.Sprintf("journalctl -u %s -n %d -o short-iso --no-pager", quote(unit), logTail)
}

// StackLogs is the log command of a compose stack, all its containers, for Stream.
// docker is the command that reaches docker on the host: Report.Docker.
func StackLogs(docker, stack string) string {
	return fmt.Sprintf("%s compose -p %s logs --tail %d --no-color", docker, quote(stack), logTail)
}

// ContainerLogs is the log command of one container, for Stream. docker: see StackLogs.
func ContainerLogs(docker, container string) string {
	return fmt.Sprintf("%s logs --tail %d %s", docker, logTail, quote(container))
}

// Logs runs a log command on h and sends its lines: the last ones, or with follow,
// those that come next too. A followed command gets a terminal, so that ending ssh
// hangs it up; any other stops on the host after oneShotTimeout. The channel closes
// when the command ends or ctx is cancelled.
func Stream(ctx context.Context, h Host, cmd string, follow bool) <-chan string {
	if follow {
		return streamLines(ctx, h, true, cmd+" -f")
	}

	return streamLines(ctx, h, false, fmt.Sprintf("timeout %d %s", oneShotTimeout, cmd))
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// streamLines runs remote on h and sends its output, stdout and stderr, line by line,
// without control characters. The channel closes when the command ends or ctx is
// cancelled. tty: see sshCmd.
func streamLines(ctx context.Context, h Host, tty bool, remote string) <-chan string {
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
