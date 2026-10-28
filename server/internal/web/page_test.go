package web

import (
	"io/fs"
	"path"
	"regexp"
	"testing"
)

// elementRef matches the element lookups of the admin page modules.
var elementRef = regexp.MustCompile(`getElementById\("([^"]*)"\)`)

// TestElementIDs checks that every element the modules look up exists in the
// admin page, so that a renamed id cannot leave a section blank silently.
func TestElementIDs(t *testing.T) {
	static, err := fs.Sub(embedded, "static")
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(static, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`\sid="([^"]*)"`).FindAllSubmatch(index, -1) {
		if ids[string(m[1])] {
			t.Errorf("index.html: id %q is not unique", m[1])
		}
		ids[string(m[1])] = true
	}
	found := 0
	err = fs.WalkDir(static, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(name) != ".js" {
			return err
		}
		src, err := fs.ReadFile(static, name)
		if err != nil {
			return err
		}
		for _, m := range elementRef.FindAllSubmatch(src, -1) {
			found++
			if !ids[string(m[1])] {
				t.Errorf("%s looks up #%s, which index.html does not have", name, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Error("no element lookups found in the admin page modules")
	}
}
