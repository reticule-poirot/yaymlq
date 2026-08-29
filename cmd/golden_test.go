package cmd

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update .golden files in testdata/golden")

// Golden tests run the real command against the checked-in testdata/*.y*ml
// fixtures and compare combined stdout to testdata/golden/<name>.golden.
// Regenerate with:  go test ./cmd -run TestGolden -update
func TestGolden(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"get-scalar", []string{"-o", "raw", ".services.web.image", "../testdata/compose.yml"}},
		{"get-wildcard-json", []string{"-o", "json", ".spec.template.spec.containers[].image", "../testdata/k8s.yaml"}},
		{"get-map-wildcard", []string{"-o", "yaml", ".metadata.labels.*", "../testdata/k8s.yaml"}},
		{"get-default", []string{"-o", "raw", "--default", "none", ".spec.strategy", "../testdata/k8s.yaml"}},
		{"set-image", []string{"set", ".services.web.image", "nginx:1.28", "../testdata/compose.yml"}},
		{"set-replace", []string{"set", ".spec.replicas", "5", "../testdata/k8s.yaml"}},
		{"set-new-key", []string{"set", ".spec.strategy.type", "RollingUpdate", "../testdata/k8s.yaml"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewRootCommand()
			var out bytes.Buffer
			c.SetOut(&out)
			c.SetErr(&out)
			c.SetIn(bytes.NewReader(nil))
			c.SetArgs(tc.args)
			if err := c.Execute(); err != nil {
				t.Fatalf("execute %v: %v", tc.args, err)
			}

			golden := filepath.Join("..", "testdata", "golden", tc.name+".golden")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if !bytes.Equal(want, out.Bytes()) {
				t.Fatalf("output mismatch for %v\n--- want ---\n%s\n--- got ---\n%s", tc.args, want, out.Bytes())
			}
		})
	}
}
