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

// DescribeMachine shows machine CPUs, its load, busiest processes, and its disks.
func DescribeMachine() string {
	return inSequence(
		"hostnamectl",
		`LC_ALL=C lscpu | grep -E '^(CPU\(s\)|Model name):'`,
		"top -b -n 2 -d 1 | awk '/^top -/ {n++} n == 2' | head -n 17",
		"df -h -x tmpfs -x devtmpfs -x overlay -x squashfs", // no pseudo filesystems, container layers or snaps
	)
}

// DescribeUnit shows a systemd unit state and last lines.
func DescribeUnit(unit string) string {
	// systemctl status exits 3 for a unit that isn't running: that's its state, not an error.
	return fmt.Sprintf("systemctl status --no-pager %s || true", quote(unit))
}

// DescribeStack shows compose stack's containers and their resource usage.
func DescribeStack(docker, stack string) string {
	project := docker + " compose -p " + quote(stack)

	return inSequence(project+" ps -a", project+" stats --no-stream")
}

// DescribeContainer shows the resources a container uses, then everything docker knows about it.
func DescribeContainer(docker, container string) string {
	return inSequence(docker+" stats --no-stream "+quote(container), docker+" inspect "+quote(container))
}

// ContainerShell is the command that opens a shell in a container, for Interactive:
// bash if the image has it, else sh. docker: see StackLogs.
func ContainerShell(docker, container string) string {
	return fmt.Sprintf("%s exec -it %s sh -c %s", docker, quote(container),
		quote("command -v bash >/dev/null && exec bash || exec sh"))
}

// inSequence runs commands one after the other, a blank line between their outputs.
func inSequence(cmds ...string) string { return strings.Join(cmds, "; echo; ") }

// Interactive is ssh running remote on h with the user's terminal, or with remote
// "", a login shell. The caller hands it the terminal.
func Interactive(ctx context.Context, h Host, remote string) *exec.Cmd {
	return sshCmd(ctx, h, true, remote)
}

// Stream runs cmd on h and sends its output lines: the last ones, or with follow,
// those that come next too. A followed command gets a terminal, so that ending ssh
// hangs it up. The channel closes when the command ends or ctx is cancelled.
func Stream(ctx context.Context, h Host, cmd string, follow bool) <-chan string {
	if follow {
		return streamLines(ctx, h, true, cmd+" -f")
	}

	return streamLines(ctx, h, false, fmt.Sprintf("timeout %d sh -c %s", oneShotTimeout, quote(cmd)))
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
