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
		// stream.yaml is a ConfigMap, a Deployment and a Service: the shape
		// --all-docs exists for, and the one that used to fail the whole run
		// because only one document has .spec.replicas (#175).
		{"get-all-docs-partial", []string{"--all-docs", "-o", "raw", ".spec.replicas", "../testdata/stream.yaml"}},
		{"paths-all-docs-json", []string{"--paths", "--all-docs", "-o", "json", ".metadata.name", "../testdata/stream.yaml"}},
		{"set-image", []string{"set", ".services.web.image", "nginx:1.28", "../testdata/compose.yml"}},
		{"set-replace", []string{"set", ".spec.replicas", "5", "../testdata/k8s.yaml"}},
		{"set-new-key", []string{"set", ".spec.strategy.type", "RollingUpdate", "../testdata/k8s.yaml"}},
		{"delete-key", []string{"delete", ".services.web.environment", "../testdata/compose.yml"}},
		{"delete-list-elem", []string{"delete", ".spec.template.spec.containers[1]", "../testdata/k8s.yaml"}},
		{"append-port", []string{"append", ".services.web.ports", "8080:8080", "../testdata/compose.yml"}},
		{"rename-key", []string{"rename", ".services.web", "webapp", "../testdata/compose.yml"}},
		{"apply-batch", []string{"apply", "-f", "../testdata/apply-edits.txt", "../testdata/compose.yml"}},
		// anchors.yml is the merge-key round trip the fixtures were missing
		// entirely before #171: an edit must leave every `<<:` alone, and a
		// key that only exists via the merge must shadow locally rather than
		// rewrite the shared anchor, which `db` also uses.
		{"set-anchored", []string{"set", ".services.web.image", "nginx:1.28", "../testdata/anchors.yml"}},
		{"set-merged-key-shadows", []string{"set", ".services.web.restart", "always", "../testdata/anchors.yml"}},
		{"append-anchored", []string{"append", ".services.web.ports", "8080:8080", "../testdata/anchors.yml"}},
		{"keys-merged", []string{"keys", ".services.web", "../testdata/anchors.yml"}},
		{"keys-map", []string{"keys", ".services.web", "../testdata/compose.yml"}},
		{"len-list", []string{"len", ".spec.template.spec.containers", "../testdata/k8s.yaml"}},
		{"type-nested", []string{"type", ".spec.template.spec.containers[0].ports", "../testdata/k8s.yaml"}},
		{"schema", []string{"schema"}},
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
