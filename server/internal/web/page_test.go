package web

import (
	"io/fs"
	"path"
	"regexp"
	"testing"
)

// elementRef matches the element lookups of the admin page modules, in
// either quotes. RE2 has no backreferences, so each quote has its own group.
var elementRef = regexp.MustCompile(`getElementById\((?:"([^"]*)"|'([^']*)')\)`)

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
			if id := string(m[1]) + string(m[2]); !ids[id] {
				t.Errorf("%s looks up #%s, which index.html does not have", name, id)
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

// TestErrorTexts checks that labels.js has the wording of every error code,
// so that a code added to errors.go is not shown without a wording
// (admin-ui.md 13).
func TestErrorTexts(t *testing.T) {
	src, err := fs.ReadFile(embedded, "static/js/labels.js")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)export const errorTexts = \{(.*?)\n\};`).FindSubmatch(src)
	if block == nil {
		t.Fatal("labels.js has no errorTexts")
	}
	keys := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(\w+):`).FindAllSubmatch(block[1], -1) {
		keys[string(m[1])] = true
	}
	for code := range errorMessages {
		if !keys[string(code)] {
			t.Errorf("labels.js errorTexts has no wording for %s", code)
		}
	}
}

// TestResetNextButton checks the reset that takes the place of the start
// button after a game (admin-ui.md 5.2): it sends reset, has the size of the
// start button, and is hidden until view.js shows it.
func TestResetNextButton(t *testing.T) {
	index, err := fs.ReadFile(embedded, "static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	tag := regexp.MustCompile(`<button\s[^>]*id="reset-next"[^>]*>`).Find(index)
	if tag == nil {
		t.Fatal("index.html has no #reset-next button")
	}
	if !regexp.MustCompile(`\sdata-action="reset"`).Match(tag) {
		t.Errorf("#reset-next does not send reset: %s", tag)
	}
	if !regexp.MustCompile(`class="[^"]*\bbtn-start\b[^"]*"`).Match(tag) {
		t.Errorf("#reset-next does not have the size of the start button: %s", tag)
	}
	if !regexp.MustCompile(`\shidden[\s>]`).Match(tag) {
		t.Errorf("#reset-next is not hidden at first: %s", tag)
	}
}
