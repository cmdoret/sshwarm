package swarm

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestLoadFlake(t *testing.T) {
	// The fake nix echoes the flake reference it was given into the hostname.
	fakeBin(t, "nix", `echo "{\"b-node\":{\"hostname\":\"$3\",\"sshUser\":\"\",\"sshOpts\":[\"-p\",\"2\"]},\"a-node\":{\"hostname\":\"h\",\"sshUser\":\"u\",\"sshOpts\":[]}}"`)

	hosts, err := LoadFlake(t.Context(), "nix")
	if err != nil {
		t.Fatal(err)
	}

	if hosts[0].Address != "u@h" || hosts[1].Name != "b-node" || !slices.Equal(hosts[1].Opts, []string{"-p", "2"}) {
		t.Errorf("%+v", hosts)
	}

	if abs, _ := filepath.Abs("nix"); hosts[1].Address != abs+"#deploy" {
		t.Errorf("the flake must be an absolute path, not a registry name: %q", hosts[1].Address)
	}

	fakeBin(t, "nix", `echo "{}"`)
	if _, err := LoadFlake(t.Context(), "."); err == nil || !strings.Contains(err.Error(), "no machines") {
		t.Errorf("empty deploy.nodes: %v", err)
	}
}

func TestLoadFile(t *testing.T) {
	load := func(content string) ([]Host, error) {
		path := filepath.Join(t.TempDir(), "hosts.toml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		return LoadFile(path)
	}

	hosts, err := load(`
[[host]]
name = "web-2"
host = "web-2.example.org"
user = "deploy"
key = "~/.ssh/web"

[[host]]
name = "db"
`)
	if err != nil {
		t.Fatal(err)
	}

	// The file's order; the host defaults to the name.
	want := []Host{
		{Name: "web-2", Address: "deploy@web-2.example.org", Opts: []string{"-i", "~/.ssh/web"}},
		{Name: "db", Address: "db"},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Errorf("got %+v\nwant %+v", hosts, want)
	}

	for content, wantErr := range map[string]string{
		"[[host]]\nnme = \"db\"":                           "unknown keys [host.nme]",
		"[[host]]\nhost = \"db\"":                          "host 1 has no name",
		"[[host]]\nname = \"db\"\n[[host]]\nname = \"db\"": "db is there twice",
		"": "no machines",
	} {
		if _, err := load(content); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: got %v, want %q", content, err, wantErr)
		}
	}
}
