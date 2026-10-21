package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

func TestValidateImageRef_LimaAccepts(t *testing.T) {
	local := filepath.Join(t.TempDir(), "my-template.yaml")
	if err := os.WriteFile(local, []byte("images: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{
		"template://ubuntu-lts",
		"https://raw.githubusercontent.com/lima-vm/lima/master/templates/ubuntu-lts.yaml",
		local,
	} {
		if err := validateImageRef("lima", ref); err != nil {
			t.Fatalf("validateImageRef(lima, %q) = %v, want nil", ref, err)
		}
	}
}

func TestValidateImageRef_LimaRejectsIncusStyle(t *testing.T) {
	for ref, want := range map[string]string{
		"images:ubuntu/24.04": "did you mean --image=template://ubuntu-24.04 ? (template://ubuntu-lts is the usual choice)",
		"images:fedora/44":    "did you mean --image=template://fedora-44 ?",
		"ubuntu:24.04":        "did you mean --image=template://ubuntu-24.04 ?",
	} {
		err := validateImageRef("lima", ref)
		if err == nil {
			t.Fatalf("validateImageRef(lima, %q) = nil, want an error", ref)
		}
		msg := err.Error()
		for _, s := range []string{`"` + ref + `"`, "template reference", "limactl template ls", want} {
			if !strings.Contains(msg, s) {
				t.Fatalf("validateImageRef(lima, %q) error %q should contain %q", ref, msg, s)
			}
		}
	}
}

func TestValidateImageRef_LimaNoSuggestionForOtherForms(t *testing.T) {
	err := validateImageRef("lima", "docker.io/library/ubuntu:24.04")
	if err == nil {
		t.Fatal("expected an error for an OCI reference on Lima")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("error %q should not guess a template for an unrecognised form", err)
	}
}

func TestValidateImageRef_IncusAcceptsEverything(t *testing.T) {
	for _, ref := range []string{"images:ubuntu/24.04", "images:fedora/44", "ubuntu:24.04", "template://ubuntu-lts"} {
		if err := validateImageRef("incus", ref); err != nil {
			t.Fatalf("validateImageRef(incus, %q) = %v, want nil", ref, err)
		}
	}
}

// executeAsLima runs the CLI with p registered under the name "lima", so
// create takes the Lima branch of validateImageRef.
func executeAsLima(t *testing.T, p provider.Provider, args ...string) (string, error) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register("lima", func() (provider.Provider, error) { return p, nil })
	root := NewRootCmd(reg)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--provider", "lima"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestCLI_Create_LimaRejectsIncusImageRef(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	_, err := executeAsLima(t, p, "create", "demo", "--image=images:ubuntu/24.04")
	if err == nil || !strings.Contains(err.Error(), "template://ubuntu-24.04") {
		t.Fatalf("create: err = %v, want the Lima template suggestion", err)
	}
	if insts, _ := p.List(t.Context()); len(insts) != 0 {
		t.Fatalf("create should not have created anything, got %v", insts)
	}
}

func TestCLI_Create_LimaAcceptsTemplateRef(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	if out, err := executeAsLima(t, p, "create", "demo", "--image=template://ubuntu-lts"); err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
}

func TestCLI_PreviewCreate_LimaRejectsIncusImageRef(t *testing.T) {
	useIsolatedConfig(t)
	p := previewingFake{fake.New()}
	out, err := executeAsLima(t, p, "--preview", "create", "demo", "--image=images:ubuntu/24.04")
	want := validateImageRef("lima", "images:ubuntu/24.04")
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("preview create: err = %v, want %v", err, want)
	}
	if strings.Contains(out, "fakectl") {
		t.Fatalf("preview should print no commands for a rejected image, got %q", out)
	}
	if insts, _ := p.List(t.Context()); len(insts) != 0 {
		t.Fatalf("preview create should not have created anything, got %v", insts)
	}
}

func TestCLI_Create_IncusAcceptsIncusImageRef(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	reg := provider.NewRegistry()
	reg.Register("incus", func() (provider.Provider, error) { return p, nil })
	root := NewRootCmd(reg)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--provider", "incus", "create", "demo", "--image=images:ubuntu/24.04"})
	if err := root.Execute(); err != nil {
		t.Fatalf("create: %v (%s)", err, out.String())
	}
}
