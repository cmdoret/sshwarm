package swarm

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoadFlake reads deploy.nodes from the flake in flakeDir, with deploy-rs's
// defaults for sshUser and sshOpts.
func LoadFlake(ctx context.Context, flakeDir string) ([]Host, error) {
	// Absolute, or nix reads a bare "nix" as the registry's github:NixOS/nix.
	dir, err := filepath.Abs(flakeDir)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "nix", "eval", "--json", dir+"#deploy", "--apply", `d: builtins.mapAttrs (_: n: {
		inherit (n) hostname;
		sshUser = n.sshUser or d.sshUser or "";
		sshOpts = n.sshOpts or d.sshOpts or [ ];
	}) d.nodes`)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nix eval: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var nodes map[string]struct {
		Hostname, SSHUser string
		SSHOpts           []string
	}
	if err := json.Unmarshal(out, &nodes); err != nil {
		return nil, fmt.Errorf("nix eval output: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("no machines in %s#deploy.nodes", dir)
	}

	var hosts []Host

	for _, name := range slices.Sorted(maps.Keys(nodes)) {
		n := nodes[name]

		addr := n.Hostname
		if n.SSHUser != "" {
			addr = n.SSHUser + "@" + addr
		}

		hosts = append(hosts, Host{Name: name, Address: addr, Opts: n.SSHOpts})
	}

	return hosts, nil
}

// LoadFile reads a TOML hosts file: a [[host]] table per machine, with its name,
// and optionally the host to reach (the name by default), the ssh user and key.
// Anything else ssh needs comes from ~/.ssh/config. The hosts keep the file's order.
func LoadFile(path string) ([]Host, error) {
	var file struct {
		Host []struct{ Name, Host, User, Key string }
	}

	md, err := toml.DecodeFile(path, &file)
	if err != nil {
		return nil, fmt.Errorf("hosts file: %w", err)
	}

	if keys := md.Undecoded(); len(keys) > 0 {
		return nil, fmt.Errorf("%s: unknown keys %v", path, keys)
	}

	if len(file.Host) == 0 {
		return nil, fmt.Errorf("no machines in %s", path)
	}

	var hosts []Host

	for i, h := range file.Host {
		if h.Name == "" {
			return nil, fmt.Errorf("%s: host %d has no name", path, i+1)
		}

		if slices.ContainsFunc(hosts, func(o Host) bool { return o.Name == h.Name }) {
			return nil, fmt.Errorf("%s: %s is there twice", path, h.Name)
		}

		addr := cmp.Or(h.Host, h.Name)
		if h.User != "" {
			addr = h.User + "@" + addr
		}

		var opts []string
		if h.Key != "" {
			opts = []string{"-i", h.Key} // ssh expands ~ itself
		}

		hosts = append(hosts, Host{Name: h.Name, Address: addr, Opts: opts})
	}

	return hosts, nil
}
