package swarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/remote"
)

const (
	// logTail is the history a logs view opens with.
	logTail = 300

	// oneShotTimeout stops a command that isn't followed, on the host, after a number
	// of seconds.
	oneShotTimeout = 30
)

// UnitLogs is the log command of a systemd unit, for Stream.
func UnitLogs(unit string) string {
	return fmt.Sprintf("journalctl -u %s -n %d -o short-iso --no-pager", quote(unit), logTail)
}

// StackLogs is the log command of a compose stack, all its containers, for Stream.
func (d Docker) StackLogs(stack string) string {
	return fmt.Sprintf("%s compose -p %s logs --tail %d --no-color", d, quote(stack), logTail)
}

// ContainerLogs is the log command of one container, for Stream.
func (d Docker) ContainerLogs(container string) string {
	return fmt.Sprintf("%s logs --tail %d %s", d, logTail, quote(container))
}

// ContainerShell is the command that opens a shell in a container, for
// remote.Interactive: bash if the image has it, else sh.
func (d Docker) ContainerShell(container string) string {
	return fmt.Sprintf("%s exec -it %s sh -c %s", d, quote(container),
		quote("command -v bash >/dev/null && exec bash || exec sh"))
}

// Verb is what an action does to a unit, a stack or a container.
type Verb string

// The actions.
const (
	Start   Verb = "start"
	Stop    Verb = "stop"
	Restart Verb = "restart"
)

// UnitAction is the command that starts, stops or restarts a systemd unit, for remote.Run.
func UnitAction(v Verb, unit string) string {
	return asRoot("systemctl --no-block " + string(v) + " " + quote(unit))
}

// Reboot is the command that reboots a machine, for remote.Run, 2 seconds later:
// once ssh has ended. It names systemctl by its full path, which systemd's own
// search path may lack (NixOS).
func Reboot() string {
	return asRoot(`systemd-run --on-active=2 --timer-property=AccuracySec=1s "$(command -v systemctl)" reboot`)
}

// asRoot is cmd, run as root: directly if the user is root, else through sudo, which
// mustn't ask for a password, as there's no terminal to ask on. In sh, whatever the
// user's shell.
func asRoot(cmd string) string {
	return "sh -c " + quote(`if [ "$(id -u)" = 0 ]; then `+cmd+"; else sudo -n "+cmd+"; fi")
}

// StackAction is the command that starts, stops or restarts a compose stack's
// containers, for remote.Run.
func (d Docker) StackAction(v Verb, stack string) string {
	return fmt.Sprintf("%s compose -p %s %s", d, quote(stack), v)
}

// ContainerAction is the command that starts, stops or restarts a container, for
// remote.Run.
func (d Docker) ContainerAction(v Verb, container string) string {
	return fmt.Sprintf("%s %s %s", d, v, quote(container))
}

// Stream runs cmd on h and sends its output lines: the last ones, or with follow,
// those that come next too, cmd being a logs command, which -f follows. A followed
// command gets a terminal, so that ending ssh hangs it up; another stops on the host
// after oneShotTimeout. The channel closes when the command ends or ctx is
// cancelled.
func Stream(ctx context.Context, h inventory.Host, cmd string, follow bool) <-chan string {
	if follow {
		return remote.Stream(ctx, h, cmd+" -f", true)
	}

	return remote.Stream(ctx, h, fmt.Sprintf("timeout %d sh -c %s", oneShotTimeout, quote(cmd)), false)
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
