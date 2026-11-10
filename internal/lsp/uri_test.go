package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileURIsRoundTripAwkwardPaths(t *testing.T) {
	for _, path := range []string{
		"/tmp/plain/a.go",
		"/tmp/with space/a.go",
		"/tmp/hash#dir/a.go",
		"/tmp/q?dir/a.go",
		"/tmp/pct%20dir/a.go",
		"/tmp/é/𝔘.go",
	} {
		uri := PathToURI(path)
		back, err := URIToPath(uri)
		if err != nil || back != path {
			t.Errorf("%q → %q → %q, %v", path, uri, back, err)
		}
	}
	if got := PathToURI("/tmp/a b/#?.go"); got != "file:///tmp/a%20b/%23%3F.go" {
		t.Errorf("PathToURI escaped as %q", got)
	}
}

func TestURIToPathRejectsNonLocalURIs(t *testing.T) {
	for _, uri := range []string{"untitled:Untitled-1", "jdt://contents/rt.jar/java.lang/Object.class", "file://server/share/a.go", "%zz"} {
		if path, err := URIToPath(uri); err == nil {
			t.Errorf("URIToPath(%q) = %q, want error", uri, path)
		}
	}
	if path, err := URIToPath("file://localhost/tmp/a.go"); err != nil || path != "/tmp/a.go" {
		t.Errorf("localhost URI = %q, %v", path, err)
	}
}

func TestRootRebasesResolvedSymlinkPaths(t *testing.T) {
	real := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(link)
	got, err := root.Path(PathToURI(filepath.Join(resolved, "pkg", "a.go")))
	if err != nil || got != filepath.Join(link, "pkg", "a.go") {
		t.Errorf("rebased path = %q, %v; want under %q", got, err, link)
	}
	outside, err := root.Path(PathToURI(resolved + "-other/a.go"))
	if err != nil || outside != resolved+"-other/a.go" {
		t.Errorf("sibling directory was rebased: %q, %v", outside, err)
	}
}
