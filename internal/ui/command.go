package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/inventory"
)

// The command bar, opened with ":", goes to a table, as k9s's does: ":ctr web-1".

// newCommandBar is the command bar, completing what's typed (commands).
func newCommandBar(hosts []inventory.Host) textinput.Model {
	c := textinput.New()
	c.Prompt = ":"
	c.ShowSuggestions, c.CompletionStyle = true, dimStyle // tab takes the one shown, ↑↓ go through the others
	c.SetSuggestions(commands(hosts))

	return c
}

// commandKey types into the command bar: enter goes there, esc cancels.
func (m *model) commandKey(k tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch {
	case key.Matches(k, keyBack):
		m.command.Blur()
		m.command.SetValue("")
	case key.Matches(k, keyEnter):
		m.command.Blur()
		cmd = m.goTo(m.command.Value())
		m.command.SetValue("")
	default:
		m.command, cmd = m.command.Update(k)
	}

	return cmd
}

// goTo opens the table a command names: a kind, then a machine by name, or else
// the machine on screen; every machine's with "all", or from the machines. Like
// k9s, it starts over from the machines, so esc goes back there; ":machines" just
// goes back.
func (m *model) goTo(command string) tea.Cmd {
	kindName, name, _ := strings.Cut(strings.TrimSpace(command), " ")
	if kindName == "" {
		return nil
	}

	k := kindNamed(kindName)
	if k == nil {
		var names []string
		for _, k := range kinds {
			names = append(names, strings.Join(k.names, "/"))
		}

		m.flash = fmt.Sprintf("no %q here: try %s", kindName, strings.Join(names, ", "))

		return nil
	}

	if k == machines {
		m.popTo(1)

		return nil
	}

	host, err := m.machine(strings.TrimSpace(name))
	if err != nil {
		m.flash = err.Error()

		return nil
	}

	if host >= 0 && m.reports[host].Err == nil && !slices.Contains(tabsOf(m.reports[host]), k) {
		m.flash = fmt.Sprintf("%s has no %s", m.hosts[host].Name, k.names[0])

		return nil
	}

	m.popTo(1)

	if host >= 0 {
		m.fleet.grid.choose(m.hosts[host].Name) // so that esc goes back to it
	}

	return m.push(newHost(k, host, ""))
}

// commands are what the command bar completes, from what's typed: every kind's
// names, alone, or but for machines', followed by all or a machine's name.
func commands(hosts []inventory.Host) []string {
	var out []string

	for _, k := range kinds {
		for _, name := range k.names {
			out = append(out, name)
			if k == machines {
				continue
			}

			out = append(out, name+" all")
			for _, h := range hosts {
				out = append(out, name+" "+h.Name)
			}
		}
	}

	return out
}

// machine is the index of the machine named, or else of the one on screen; -1 for
// every machine: named "all", or none on screen.
func (m *model) machine(name string) (int, error) {
	switch {
	case name == "all":
		return -1, nil
	case name != "":
		return inventory.Find(m.hosts, name)
	}

	for _, s := range slices.Backward(m.screens) {
		if h, ok := s.(*host); ok {
			return h.machine, nil
		}
	}

	return -1, nil
}
