package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// toolsEnv is a PATH directory holding only fake commands, plus the real
// ln, env and true that the fake sudo hands its command to.
type toolsEnv struct {
	t      *testing.T
	sh     string
	bin    string // the whole of PATH
	link   string // agentctl_tools_bindir: where the fd symlink goes
	logged string // every fake package-manager call, one per line
}

func newToolsEnv(t *testing.T) *toolsEnv {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	e := &toolsEnv{t: t, sh: sh, bin: t.TempDir(), link: t.TempDir()}
	e.logged = filepath.Join(t.TempDir(), "calls")
	for _, tool := range []string{"ln", "env", "true"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s on PATH", tool)
		}
		if err := os.Symlink(real, filepath.Join(e.bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	e.fake("sudo", `[ "$1" = -n ] && shift; exec "$@"`)
	return e
}

// fake puts an executable called name on PATH that runs body.
func (e *toolsEnv) fake(name, body string) {
	e.t.Helper()
	script := "#!" + e.sh + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(e.bin, name), []byte(script), 0o755); err != nil {
		e.t.Fatal(err)
	}
}

// manager fakes a package manager that logs its arguments and exits code.
func (e *toolsEnv) manager(name, code string) {
	e.fake(name, `echo "`+name+` $*" >>`+e.logged+`; exit `+code)
}

func (e *toolsEnv) run(body string) (string, error) {
	e.t.Helper()
	cmd := exec.Command(e.sh, "-c", toolsBootstrap+"agentctl_tools_bindir="+e.link+"\n"+body)
	cmd.Env = []string{"PATH=" + e.bin}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *toolsEnv) calls() string {
	data, _ := os.ReadFile(e.logged)
	return string(data)
}

func TestToolsBootstrap_PkgMgrOrder(t *testing.T) {
	cases := []struct {
		have []string
		want string
	}{
		{[]string{"apt-get", "apk", "dnf"}, "apt"},
		{[]string{"apk", "dnf"}, "apk"},
		{[]string{"dnf"}, "dnf"},
		{nil, ""},
	}
	for _, c := range cases {
		e := newToolsEnv(t)
		for _, m := range c.have {
			e.manager(m, "0")
		}
		out, err := e.run("agentctl_tools_pkgmgr")
		if err != nil {
			t.Fatalf("%v: %v\n%s", c.have, err, out)
		}
		if got := strings.TrimSpace(out); got != c.want {
			t.Errorf("with %v: agentctl_tools_pkgmgr = %q, want %q", c.have, got, c.want)
		}
	}
}

func TestToolsBootstrap_InstallCommands(t *testing.T) {
	cases := []struct{ manager, want string }{
		{"apt-get", "apt-get install -y -qq ripgrep fd-find"},
		{"apk", "apk add --no-cache ripgrep fd"},
		{"dnf", "dnf install -y ripgrep fd-find"},
	}
	for _, c := range cases {
		e := newToolsEnv(t)
		e.manager(c.manager, "0")
		out, err := e.run("agentctl_ensure_tools pi")
		if err != nil {
			t.Fatalf("%s: %v\n%s", c.manager, err, out)
		}
		if !strings.Contains(e.calls(), c.want+"\n") {
			t.Errorf("%s: calls %q, want one to be %q", c.manager, e.calls(), c.want)
		}
	}
}

// Debian's fd-find installs the binary as fdfind; agentctl links fd to it
// only when fd is still missing afterwards.
func TestToolsBootstrap_FdfindSymlink(t *testing.T) {
	for _, haveFd := range []bool{false, true} {
		e := newToolsEnv(t)
		e.manager("apt-get", "0")
		e.fake("fdfind", "true")
		if haveFd {
			e.fake("fd", "true")
		}
		if out, err := e.run("agentctl_ensure_tools pi"); err != nil {
			t.Fatalf("fd present %v: %v\n%s", haveFd, err, out)
		}
		target, err := os.Readlink(filepath.Join(e.link, "fd"))
		switch {
		case haveFd && err == nil:
			t.Errorf("fd already on PATH, but agentctl linked %s/fd anyway", e.link)
		case !haveFd && err != nil:
			t.Errorf("fd missing and fdfind present, but no %s/fd symlink: %v", e.link, err)
		case !haveFd && target != filepath.Join(e.bin, "fdfind"):
			t.Errorf("%s/fd links to %q, want %q", e.link, target, filepath.Join(e.bin, "fdfind"))
		}
	}
}

func TestToolsBootstrap_SkipsWhenPresent(t *testing.T) {
	e := newToolsEnv(t)
	e.manager("apt-get", "0")
	e.fake("rg", "true")
	e.fake("fd", "true")
	if out, err := e.run("agentctl_ensure_tools pi"); err != nil || out != "" {
		t.Fatalf("want a silent no-op, got %v\n%s", err, out)
	}
	if e.calls() != "" {
		t.Errorf("rg and fd on PATH, but the package manager ran: %q", e.calls())
	}
}

// Missing tools must never fail the agent install: each path prints one
// "agentctl: pi:" line and pi's installer still runs.
func TestToolsBootstrap_FailuresContinue(t *testing.T) {
	cases := []struct {
		name, want string
		setup      func(*toolsEnv)
	}{
		{"apt fails", "agentctl: pi: could not install ripgrep and fd (apt-get exited 100); create the instance with --allow-preset apt",
			func(e *toolsEnv) { e.manager("apt-get", "100") }},
		{"apk fails", "agentctl: pi: could not install ripgrep and fd (apk add exited 1);",
			func(e *toolsEnv) { e.manager("apk", "1") }},
		{"no manager", "agentctl: pi: could not install ripgrep and fd (no apt-get, apk or dnf in the guest);",
			func(e *toolsEnv) {}},
		{"no sudo", "agentctl: pi: not installing ripgrep and fd (no passwordless sudo);",
			func(e *toolsEnv) { e.manager("apt-get", "0"); e.fake("sudo", "exit 1") }},
	}
	for _, c := range cases {
		e := newToolsEnv(t)
		c.setup(e)
		out, err := e.run("agentctl_ensure_tools pi\necho PI")
		if err != nil || !strings.HasSuffix(out, "PI\n") {
			t.Errorf("%s: want pi's installer to still run, got %v\n%s", c.name, err, out)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: output %q does not contain %q", c.name, out, c.want)
		}
		if n := strings.Count(out, "agentctl: pi:"); n != 1 {
			t.Errorf("%s: %d agentctl: pi: lines, want 1\n%s", c.name, n, out)
		}
		if c.name == "no sudo" && e.calls() != "" {
			t.Errorf("no sudo, but the package manager ran: %q", e.calls())
		}
	}
}
