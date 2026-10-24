package ymledit_test

import (
	"errors"
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"gopkg.in/yaml.v3"
)

// TestEditsRefuseDescend: every editing verb refuses `**` for the same reason
// it refuses `*` — an edit has to name one place, and a path that fans out
// names none. ErrUnsupported rather than the "path didn't resolve" family,
// since it is wrong about the edit and so true of every document, which is
// what keeps --all-docs from skipping it document by document (#170).
func TestEditsRefuseDescend(t *testing.T) {
	for name, expr := range map[string]string{
		"leading":  "**.image",
		"interior": "spec.**.image",
		"trailing": "spec.**",
	} {
		t.Run(name, func(t *testing.T) {
			segs, err := path.Parse(expr)
			if err != nil {
				t.Fatalf("Parse(%q): %v", expr, err)
			}
			for verb, edit := range map[string]func(*yaml.Node) error{
				"set": func(d *yaml.Node) error {
					return ymledit.Set(d, segs, &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}, nil)
				},
				"append": func(d *yaml.Node) error {
					return ymledit.Append(d, segs, &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}, nil)
				},
				"delete": func(d *yaml.Node) error { return ymledit.Delete(d, segs, nil) },
				"rename": func(d *yaml.Node) error { return ymledit.Rename(d, segs, "other", nil) },
			} {
				t.Run(verb, func(t *testing.T) {
					var doc yaml.Node
					if err := yaml.Unmarshal([]byte("spec:\n  image: a\n"), &doc); err != nil {
						t.Fatal(err)
					}
					err := edit(&doc)
					if err == nil {
						t.Fatalf("%s accepted %q", verb, expr)
					}
					if !errors.Is(err, ymledit.ErrUnsupported) {
						t.Errorf("want ErrUnsupported, got %v", err)
					}
					if errors.Is(err, ymledit.ErrNotResolved) {
						t.Error("a refusal about the edit must not look like a path that didn't resolve")
					}
				})
			}
		})
	}
}

// TestEditsStillReachALiteralDoubleStarKey: reserving the spelling must not
// make the key unreachable — the quoted form still addresses it.
func TestEditsStillReachALiteralDoubleStarKey(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("\"**\": old\n"), &doc); err != nil {
		t.Fatal(err)
	}
	segs, err := path.Parse(`"**"`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := ymledit.Set(&doc, segs, &yaml.Node{Kind: yaml.ScalarNode, Value: "new"}, nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\"**\": new\n"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}
