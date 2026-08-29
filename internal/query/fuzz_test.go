package query_test

import (
	"testing"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"gopkg.in/yaml.v3"
)

// FuzzRun checks that resolving an arbitrary path against a real document never
// panics.
func FuzzRun(f *testing.F) {
	const seedDoc = `
name: yaymlq
nested: {a: {b: hello}}
services:
  - {name: web, ports: [80, 443]}
  - {name: db, ports: [5432]}
`
	var doc any
	if err := yaml.Unmarshal([]byte(seedDoc), &doc); err != nil {
		f.Fatalf("seed doc: %v", err)
	}
	for _, s := range []string{"", "nested.a.b", "services.*.ports[]", "services[99]", "x.y.z", "services.*.*.*"} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, expr string) {
		_, _ = query.Run(doc, expr)
	})
}
