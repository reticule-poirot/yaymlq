package cmd

import "testing"

// TestEditLeavesAnEncodedBlockBodyAlone guards #277 across every pass that
// scans the encoder's output by line. Each case is a block scalar the encoder
// renders itself — one blockscalars.go declined, or a block the edit brings
// in — holding a line shaped like what that pass repairs. The line is content,
// so the value it belongs to must come through the edit unchanged.
func TestEditLeavesAnEncodedBlockBodyAlone(t *testing.T) {
	cases := []struct {
		name, in, path string
		args           []string
	}{
		{
			name: "stranded token",
			in:   "f:\n :#!\n\n !\nafter: 1\n",
			path: ".f",
			args: []string{"set", ".after", "2"},
		},
		{
			name: "tab-only line, declined source block",
			in:   "f:\n  |-\n  a\n  \t\n  b\nx: 1\n",
			path: ".f",
			args: []string{"set", ".x", "2"},
		},
		{
			name: "tab-only line, block brought in by the edit",
			in:   "f: 0\n",
			path: ".f",
			args: []string{"set", ".f", "|-\n a\n \t\n b"},
		},
		{
			name: "stranded dash, declined source block",
			in:   "f:\n  |-\n  a\n  -\t\n     b\nx: 1\n",
			path: ".f",
			args: []string{"set", ".x", "2"},
		},
		{
			name: "stranded dash, block brought in by the edit",
			in:   "f: 0\n",
			path: ".f",
			args: []string{"set", ".f", "|-\n a\n -\t\n    b"},
		},
		{
			name: "comment gutter, declined source block",
			in:   "f:\n  |-\n  a\n  b # c\nx: 1   # c\n",
			path: ".f",
			args: []string{"set", ".x", "2"},
		},
		{
			name: "comment gutter, block brought in by the edit",
			in:   "f: 0\nx: 1   # c\n",
			path: ".f",
			args: []string{"set", ".f", "|-\n a\n b # c"},
		},
		{
			name: "lone dash marker",
			in:   "s:\n  -\n    a: 1\nf: 0\n",
			path: ".f",
			args: []string{"set", ".f", "|-\n x\n y # yaymlqLoneDash"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := ""
			if tc.args[1] == tc.path {
				// The edit sets the value itself; what it should read back as
				// is what the set value decodes to.
				var err error
				want, err = execute(t, "v: "+tc.args[2]+"\n", "-o", "json", ".v")
				if err != nil {
					t.Fatalf("decode the set value: %v", err)
				}
			} else {
				var err error
				want, err = execute(t, tc.in, "-o", "json", tc.path)
				if err != nil {
					t.Fatalf("read before: %v", err)
				}
			}
			edited, err := execute(t, tc.in, tc.args...)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			got, err := execute(t, edited, "-o", "json", tc.path)
			if err != nil {
				t.Fatalf("read after: %v\n%q", err, edited)
			}
			if got != want {
				t.Fatalf("%s changed: want %s got %s\n%q", tc.path, want, got, edited)
			}
		})
	}
}
