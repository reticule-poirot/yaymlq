package cmd

import (
	"strings"
	"testing"
)

// assertRefusedAsParse holds a read and an edit to the same refusal: exit 2
// with want in the message, so neither half accepts what the other refuses.
func assertRefusedAsParse(t *testing.T, src, want string) {
	t.Helper()
	for _, args := range [][]string{{"--all-docs", "."}, {"set", "--all-docs", ".z", "1"}} {
		out, err := execute(t, src, args...)
		if err == nil {
			t.Fatalf("%v: want a refusal, got %q", args, out)
		}
		if codeFor(err) != 2 {
			t.Errorf("%v: codeFor = %d, want 2 (parse); err = %v", args, codeFor(err), err)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%v: error should say %q, got %q", args, want, err)
		}
	}
	out, err := execute(t, src, "validate")
	if err == nil || !strings.Contains(out, want) {
		t.Errorf("validate: err = %v, output %q; want a failure saying %q", err, out, want)
	}
}

// assertReads holds a read and an edit to accepting src.
func assertReads(t *testing.T, src string) {
	t.Helper()
	for _, args := range [][]string{{"--all-docs", "."}, {"validate"}} {
		if out, err := execute(t, src, args...); err != nil {
			t.Errorf("%v: %v\n%s", args, err, out)
		}
	}
}

// TestLongImplicitKeyIsRefused: YAML 1.2.2 [154] and [155] limit an implicit
// key to 1024 characters, on a block mapping's key ([193]) and on a flow
// sequence's single pair ([152], [153]). The count runs to the ":", separation
// included, and is in characters rather than bytes.
func TestLongImplicitKeyIsRefused(t *testing.T) {
	k := strings.Repeat("k", 1025)
	for name, src := range map[string]string{
		"plain":                 k + ": 1\n",
		"double-quoted":         `"` + strings.Repeat("k", 1100) + "\": 1\n",
		"single-quoted":         "'" + strings.Repeat("k", 1100) + "': 1\n",
		"an escape in a quote":  `"` + strings.Repeat(`\x41`, 300) + "\": 1\n",
		"separation counts":     strings.Repeat("k", 1024) + " : 1\n",
		"multi-byte characters": strings.Repeat("é", 1025) + ": 1\n",
		"nested":                "a:\n  " + k + ": 1\n",
		"in a block sequence":   "- " + k + ": 1\n",
		"anchored":              "&a " + k + ": 1\n",
		"tagged":                "!!str " + k + ": 1\n",
		"flow sequence pair":    "[" + k + ": 1]\n",
		"second flow pair":      "[a: 1, " + k + ": 1]\n",
		"after multi-byte text": "[é, " + k + ": 1]\n",
		"after another line":    "a: 1\n" + k + ": 1\n",
		"mid-stream":            "a: 1\n---\n" + k + ": 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			assertRefusedAsParse(t, src, "1024")
		})
	}
}

// TestLongKeyTheLimitDoesNotReachIsRead: the limit is on implicit keys only. A
// flow mapping's entry (ns-flow-map-yaml-key-entry) and an explicit key carry none.
func TestLongKeyTheLimitDoesNotReachIsRead(t *testing.T) {
	k := strings.Repeat("k", 1100)
	for name, src := range map[string]string{
		"exactly at the limit":        strings.Repeat("k", 1024) + ": 1\n",
		"multi-byte, at the limit":    strings.Repeat("é", 1024) + ": 1\n",
		"explicit":                    "? " + k + "\n: 1\n",
		"explicit in a sequence":      "- ? " + k + "\n  : 1\n",
		"flow mapping":                "{" + k + ": 1}\n",
		"flow mapping, quoted":        `{"` + k + "\": 1}\n",
		"flow mapping in a sequence":  "[{" + k + ": 1}]\n",
		"tagged flow mapping":         "!!map {" + k + ": 1}\n",
		"explicit in a flow mapping":  "{? " + k + " : 1}\n",
		"explicit flow sequence pair": "[? " + k + " : 1]\n",
		"a key with no colon":         "{" + k + "}\n",
		"a long value":                "a: " + k + "\n",
		"a long comment":              "a: 1 # " + k + "\n",
		"multi-line flow mapping key": "{\"a\n  b\": 1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			assertReads(t, src)
		})
	}
}

// TestEditDoesNotWriteALongImplicitKey: the refusal would otherwise turn an
// edit's own output into a file the next run refuses.
func TestEditDoesNotWriteALongImplicitKey(t *testing.T) {
	k := strings.Repeat("k", 1100)
	got, err := execute(t, "a: 1\n", "set", "."+k, "2")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if out, err := execute(t, got, "-o", "raw", "."+k); err != nil || out != "2\n" {
		t.Errorf("reading back %q: %q, %v", got, out, err)
	}
	got, err = execute(t, "a: 1\n", "rename", ".a", k)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if out, err := execute(t, got, "-o", "raw", "."+k); err != nil || out != "1\n" {
		t.Errorf("reading back %q: %q, %v", got, out, err)
	}
}

// TestRawControlCharacterOutsideQuotesIsRefused: DEL and the C1 block other than
// NEL are outside c-printable ([1]), so outside nb-char ([27]) and ns-char ([34]),
// which plain scalars, block scalars and comments are made of.
func TestRawControlCharacterOutsideQuotesIsRefused(t *testing.T) {
	for _, c := range []string{"\x7f", "\u0080", "\u0084", "\u0086", "\u009f"} {
		for name, src := range map[string]string{
			"plain value":   "a: x" + c + "y\n",
			"plain key":     "x" + c + "y: 1\n",
			"plain root":    "x" + c + "y\n",
			"plain item":    "- x" + c + "y\n",
			"in flow":       "[x" + c + "y]\n",
			"tagged plain":  "a: !!str x" + c + "y\n",
			"literal":       "a: |\n  x" + c + "y\n",
			"folded":        "a: >\n  x" + c + "y\n",
			"head comment":  "# x" + c + "y\na: 1\n",
			"line comment":  "a: 1 # x" + c + "y\n",
			"foot comment":  "a: 1\n# x" + c + "y\n\nb: 2\n",
			"final comment": "a: 1\n# x" + c + "y\n",
		} {
			t.Run(name+"/"+strings.ToValidUTF8(c, "?"), func(t *testing.T) {
				assertRefusedAsParse(t, src, "control character")
			})
		}
	}
}

// TestControlCharacterTheSpecAllowsIsRead: §5.1 and [2] make every non-C0
// character legal in a quoted scalar, and an escape is legal anywhere a double
// quote is.
func TestControlCharacterTheSpecAllowsIsRead(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"raw DEL, double-quoted": {"a: \"x\x7fy\"\n", "x\x7fy\n"},
		"raw DEL, single-quoted": {"a: 'x\x7fy'\n", "x\x7fy\n"},
		"raw C1, double-quoted":  {"a: \"x\u0080y\"\n", "x\u0080y\n"},
		"raw C1, single-quoted":  {"a: 'x\u009fy'\n", "x\u009fy\n"},
		"escaped DEL":            {`a: "x\x7Fy"` + "\n", "x\x7fy\n"},
		"escaped C1":             {`a: "x\u009Fy"` + "\n", "x\u009fy\n"},
		"printable neighbours":   {"a: ~\u00a0\u00a1\n", "~\u00a0\u00a1\n"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := execute(t, tc.src, "-o", "raw", ".a")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("a quoted key", func(t *testing.T) {
		assertReads(t, "\"x\x7fy\": 1\n")
	})
	t.Run("an edit keeps it", func(t *testing.T) {
		in := "a: \"x\x7fy\"\nb: 1\n"
		got, err := execute(t, in, "set", ".b", "2")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if want := "a: \"x\x7fy\"\nb: 2\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// TestInLineCollectionAfterAnEmptyKeyIsRefused: [194] gives an implicit key's
// value s-l+block-node, not the s-l+block-indented that allows a compact form,
// so a block collection cannot start on the line of the ":".
func TestInLineCollectionAfterAnEmptyKeyIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		"mapping":          ": b: c\n",
		"sequence":         ": - a\n",
		"after a key":      "a: 1\n: b: c\n",
		"nested":           "x:\n  : b: c\n",
		"in a sequence":    "- : b: c\n",
		"anchored mapping": ": &a b: c\n",
	} {
		t.Run(name, func(t *testing.T) {
			assertRefusedAsParse(t, src, "empty key")
		})
	}
}

// TestEmptyKeyTheGrammarAllowsIsRead: [192] allows an empty node as an implicit
// key, and the compact form is legal after an explicit one ([191]).
func TestEmptyKeyTheGrammarAllowsIsRead(t *testing.T) {
	for name, src := range map[string]string{
		"scalar value":                ": b\n",
		"no value":                    ":\n",
		"block scalar value":          ": |\n  x\n",
		"flow value":                  ": {b: c}\n",
		"collection on the next line": ":\n  b: c\n",
		"tag above the collection":    ": !!map\n  b: c\n",
		"anchor above the collection": ": &a\n  b: c\n",
		"in a flow mapping":           "{: b}\n",
		"explicit empty key":          "? \n: b: c\n",
		"explicit empty, nested":      "- ? \n  : b: c\n",
		"explicit named key":          "? a\n: b: c\n",
		"explicit named, sequence":    "? a\n: - b\n",
	} {
		t.Run(name, func(t *testing.T) {
			assertReads(t, src)
		})
	}
}

// TestPlainNegativeZeroIsAnInt: `-0` matches the Core schema's [-+]? [0-9]+, and
// int comes before float in its table (§10.3.2). yaml.v4 alone resolves this
// spelling to a float: `+0` and `-00` are already ints.
func TestPlainNegativeZeroIsAnInt(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"plain":              {"a: -0\n", "0\n"},
		"explicitly int":     {"a: !!int -0\n", "0\n"},
		"explicitly float":   {"a: !!float -0\n", "-0\n"},
		"written as a float": {"a: -0.0\n", "-0\n"},
		"quoted":             {"a: \"-0\"\n", "-0\n"},
		"through an alias":   {"x: &x -0\na: *x\n", "0\n"},
		"plus":               {"a: +0\n", "0\n"},
		"leading zero":       {"a: -00\n", "0\n"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := execute(t, tc.src, "-o", "raw", ".a")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("--default", func(t *testing.T) {
		got, err := execute(t, "a: 1\n", "-o", "raw", "--default", "-0", ".missing")
		if err != nil || got != "0\n" {
			t.Errorf("got %q, %v; want 0", got, err)
		}
	})
	t.Run("a key", func(t *testing.T) {
		got, err := execute(t, "-0: a\n", "-o", "json", ".")
		if err != nil || !strings.Contains(got, `"0"`) {
			t.Errorf("got %q, %v; want the key 0", got, err)
		}
	})
	t.Run("an edit keeps the spelling", func(t *testing.T) {
		got, err := execute(t, "a: -0\nb: 1\n", "set", ".b", "2")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if want := "a: -0\nb: 2\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// TestNegativeZeroKeyDuplicatesZero: as an int, `-0` is the key `0`, and a read
// and an edit refuse the two alike.
func TestNegativeZeroKeyDuplicatesZero(t *testing.T) {
	assertRefusedAsParse(t, "0: a\n-0: b\n", "already defined")
}
