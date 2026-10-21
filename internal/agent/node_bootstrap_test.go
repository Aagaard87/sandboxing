package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The pi install script runs under the guest's /bin/sh, which on Alpine
// and Debian is not bash, so a syntax error here would surface as a
// failed `create --agent=pi` on someone else's machine.
func TestPiInstallScript_IsValidPOSIXShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH; cannot syntax-check the install script")
	}
	cmd := exec.Command(sh, "-n", "-c", Registry["pi"].InstallScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("sh -n rejected pi's install script: %v\n%s", err, out)
	}
}

// pi's own one-liner must still run unchanged after Node, and only its
// success may lead on to putting pi on PATH.
func TestPiInstallScript_RunsPiInstallerAfterNode(t *testing.T) {
	script := Registry["pi"].InstallScript
	if !strings.HasPrefix(script, nodeBootstrap) {
		t.Error("pi's install script does not start with the Node bootstrap")
	}
	want := "agentctl_ensure_node pi 22.19.0\n" +
		"curl -fsSL https://pi.dev/install.sh | sh || exit\n" +
		"agentctl_agent_on_path pi .pi/agent/bin"
	if !strings.HasSuffix(script, want) {
		t.Errorf("pi's install script should end with %q", want)
	}
}

// runBootstrap runs the bootstrap's function definitions, then body, under
// sh with PATH holding only a temporary directory — so the guest's node,
// if any, is out of reach — and, when nodeVersion is non-empty, a fake
// node there printing it. The bootstrap's version handling is pure shell,
// so it needs nothing else on PATH.
func runBootstrap(t *testing.T, nodeVersion, body string) (string, error) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	dir := t.TempDir()
	if nodeVersion != "" {
		fake := "#!" + sh + "\necho " + nodeVersion + "\n"
		if err := os.WriteFile(filepath.Join(dir, "node"), []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(sh, "-c", nodeBootstrap+body)
	cmd.Env = []string{"PATH=" + dir, "HOME=" + dir}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestNodeBootstrap_VersionGE(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v22.19.0", "22.19.0", true},
		{"v22.18.9", "22.19.0", false},
		{"v24.1.0", "22.19.0", true},
		{"v22.100.0", "22.19.0", true}, // a string comparison gets this wrong
		{"v9.0.0", "22.19.0", false},   // and this
		{"v22.19.1", "22.19.0", true},
		{"v23.0.0-nightly2025", "22.19.0", true},
		{"", "22.19.0", false},
		{"garbage", "22.19.0", false},
	}
	for _, c := range cases {
		_, err := runBootstrap(t, "", "agentctl_version_ge '"+c.a+"' '"+c.b+"'")
		if got := err == nil; got != c.want {
			t.Errorf("agentctl_version_ge %q %q = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestNodeBootstrap_Decision checks when agentctl_ensure_node installs
// Node, with both install paths stubbed out so nothing is downloaded. The
// stubs install nothing, so after one runs the re-check still fails.
func TestNodeBootstrap_Decision(t *testing.T) {
	const stub = "agentctl_node_install_tarball() { echo INSTALL; }\n" +
		"agentctl_node_install_apk() { echo INSTALL; }\n" +
		"agentctl_ensure_node pi 22.19.0\necho PI\n"
	cases := []struct {
		node    string
		install bool
		failMsg string // empty: succeeds and reaches pi's installer
	}{
		{"v22.19.0", false, ""},
		{"v24.1.0", false, ""},
		{"v22.18.9", true, "agentctl: pi: installed Node.js is v22.18.9, but pi needs 22.19.0 or newer"},
		{"", true, "agentctl: pi: node is still not on PATH after installing Node.js"},
	}
	for _, c := range cases {
		out, err := runBootstrap(t, c.node, stub)
		if got := strings.Contains(out, "INSTALL"); got != c.install {
			t.Errorf("node %q: installed = %v, want %v\n%s", c.node, got, c.install, out)
		}
		if c.failMsg == "" {
			if err != nil || !strings.Contains(out, "PI") {
				t.Errorf("node %q: want success reaching pi's installer, got %v\n%s", c.node, err, out)
			}
			continue
		}
		if err == nil || strings.Contains(out, "PI") {
			t.Errorf("node %q: want failure before pi's installer, got %v\n%s", c.node, err, out)
		}
		if !strings.Contains(out, c.failMsg) {
			t.Errorf("node %q: output %q does not contain %q", c.node, out, c.failMsg)
		}
	}
}

// runPathStep runs agentctl_agent_on_path for pi with HOME set to home and
// the host's PATH (the ~/.profile guard needs grep), then prints PATH.
func runPathStep(t *testing.T, home string) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	cmd := exec.Command(sh, "-c", nodeBootstrap+"agentctl_agent_on_path pi .pi/agent/bin\necho \"PATH=$PATH\"")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("agentctl_agent_on_path failed, but it must never fail the install: %v\n%s", err, out)
	}
	return string(out)
}

func TestNodeBootstrap_AgentOnPath(t *testing.T) {
	const line = `export PATH="$HOME/.pi/agent/bin:$PATH"`
	profile := func(home string) string {
		data, _ := os.ReadFile(filepath.Join(home, ".profile"))
		return string(data)
	}

	home := t.TempDir()
	bin := filepath.Join(home, ".pi", "agent", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("# skel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runPathStep(t, home)
	if !strings.Contains(out, "PATH="+bin+":") {
		t.Errorf("rest of the script does not get %s first on PATH:\n%s", bin, out)
	}
	if strings.Contains(out, "agentctl: pi:") {
		t.Errorf("binary present, but got a notice:\n%s", out)
	}
	if want := "# skel\n" + line + "\n"; profile(home) != want {
		t.Errorf("~/.profile = %q, want %q", profile(home), want)
	}
	runPathStep(t, home)
	if n := strings.Count(profile(home), line); n != 1 {
		t.Errorf("after two runs ~/.profile has the PATH line %d times, want 1:\n%s", n, profile(home))
	}

	// No binary where pi installs it: a notice, no PATH line, no failure.
	empty := t.TempDir()
	out = runPathStep(t, empty)
	if !strings.Contains(out, "agentctl: pi: no pi at "+empty+"/.pi/agent/bin/pi") {
		t.Errorf("binary missing, but no agentctl: pi: notice:\n%s", out)
	}
	if got := profile(empty); got != "" {
		t.Errorf("binary missing, but ~/.profile = %q", got)
	}
}
