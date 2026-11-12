package ui

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// PrintStatus writes the machines table, then what's wrong.
// It returns false if any machine is unreachable or has a problem.
func PrintStatus(w io.Writer, reports []swarm.Report) bool {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	columns := len(machineTitles) - 1 // PROBLEMS go below the table
	fmt.Fprintln(tw, strings.Join(machineTitles[:columns], "\t"))

	var details []string

	for _, r := range reports {
		var cells []string
		for _, c := range machineRow(r).cells[:columns] {
			cells = append(cells, c.text)
		}

		fmt.Fprintln(tw, strings.Join(cells, "\t"))

		sep := " "
		if r.Err != nil {
			sep = ": "
		}

		for _, p := range r.Problems() {
			details = append(details, r.Host.Name+sep+p)
		}
	}

	tw.Flush()

	if len(details) > 0 {
		fmt.Fprintf(w, "\n%s\n", strings.Join(details, "\n"))
	}

	return len(details) == 0
}
