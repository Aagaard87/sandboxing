package cli

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Incus-style references a user is likely to bring to Lima from the
// user guide or from Incus habit: "images:ubuntu/24.04" (the simplestreams
// remote) and the shorter "ubuntu:24.04". Both name a distro and a
// version, which is enough to suggest the matching Lima template.
var (
	incusRemoteImageRef = regexp.MustCompile(`^images:([A-Za-z0-9][A-Za-z0-9.-]*)/([A-Za-z0-9][A-Za-z0-9._-]*)$`)
	distroVersionRef    = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9.-]*):([A-Za-z0-9][A-Za-z0-9._-]*)$`)
)

// validateImageRef rejects an image reference the backend cannot use,
// before create does any work. Only Lima is checked: it has no image
// store (see docs/admin/capability-matrix.md) and passes the reference
// straight to `limactl create`, which fails opaquely on anything that
// isn't a template reference, a URL, or a local template file. Incus
// image references are open-ended and correct as given, so other
// backends accept everything.
//
// The reference is never rewritten: a wrong guess at the matching
// template would create the wrong guest. The error suggests one instead.
func validateImageRef(providerName, ref string) error {
	if providerName != "lima" {
		return nil
	}
	// "template://name" is the documented form; "template:name" is
	// Lima's newer spelling of the same thing.
	if strings.HasPrefix(ref, "template:") {
		return nil
	}
	if u, err := url.Parse(ref); err == nil && u.Host != "" {
		switch u.Scheme {
		case "http", "https":
			return nil
		}
	}
	if strings.HasPrefix(ref, "file://") {
		return nil
	}
	if _, err := os.Stat(ref); err == nil {
		return nil
	}

	msg := fmt.Sprintf("image %q is not a Lima image reference: the Lima backend takes a template reference "+
		"(template://<name>; `limactl template ls` lists them), a URL, or a local template path", ref)
	m := incusRemoteImageRef.FindStringSubmatch(ref)
	if m == nil {
		m = distroVersionRef.FindStringSubmatch(ref)
	}
	if m != nil {
		msg += fmt.Sprintf("; did you mean --image=template://%s-%s ? (template://ubuntu-lts is the usual choice)",
			strings.ToLower(m[1]), strings.ToLower(m[2]))
	}
	return fmt.Errorf("%s", msg)
}
