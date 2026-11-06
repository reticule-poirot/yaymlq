package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"go.yaml.in/yaml/v4"
)

// validOutputFormat reports whether format is a value -o/--output accepts.
// Checked up front by run() and runInspect() — before any output is
// produced, and regardless of --quiet — so an invalid value is a usage
// error (exit 3) rather than surfacing later as an unclassified render()
// failure (exit 1) that --quiet can also skip past entirely, since --quiet
// never calls render() at all.
func validOutputFormat(format string) bool {
	switch format {
	case "", "yaml", "yml", "json", "raw":
		return true
	default:
		return false
	}
}

// render writes value to w in the requested format (yaml|json|raw).
func render(w io.Writer, value any, format string) error {
	switch format {
	case "", "yaml", "yml":
		if err := checkYAMLKeys(value); err != nil {
			return err
		}
		return encodeYAML(w, stableKeys(value), 2)
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		out, name, collided := jsonKeyed(value)
		if collided {
			return usageErr(fmt.Errorf("-o json: two keys in one mapping are both spelled %q, which a JSON object cannot hold; rename one of them", name))
		}
		return enc.Encode(out)
	case "raw":
		return renderRaw(w, value)
	default:
		return fmt.Errorf("unknown output format %q (want yaml|json|raw)", format)
	}
}

// stableKeys returns what to encode for value so the output is the same on
// every run. The encoder sorts a map's keys with sort.Sort over Go's random
// iteration order, and its natural-sort comparison is a strict order for every
// key but those unstableKey picks out: with one of them it can say a < b < c <
// a, and the result then follows the iteration. A value holding none is
// returned as is, so its output is exactly the encoder's.
func stableKeys(value any) any {
	if !holdsUnstableKey(value) {
		return value
	}
	return rankKeys(value)
}

// longDigits is a digit run that might not fit the encoder's int64.
var longDigits = regexp.MustCompile(`[0-9]{19,}`)

// unstableKey reports whether k can break the encoder's key comparison. A digit
// run past int64 wraps negative. A non-ASCII digit is read as its distance from
// '0', so runs of them compare out of order. NaN is neither less nor greater
// than any number.
func unstableKey(k any) bool {
	switch k := k.(type) {
	case float64:
		return math.IsNaN(k)
	case string:
		for _, r := range k {
			if r > unicode.MaxASCII && unicode.IsDigit(r) {
				return true
			}
		}
		for _, run := range longDigits.FindAllString(k, -1) {
			if _, err := strconv.ParseInt(run, 10, 64); err != nil {
				return true
			}
		}
	}
	return false
}

func holdsUnstableKey(v any) bool {
	switch c := v.(type) {
	case map[string]any:
		for k, e := range c {
			if unstableKey(k) || holdsUnstableKey(e) {
				return true
			}
		}
	case map[any]any:
		for k, e := range c {
			if unstableKey(k) || holdsUnstableKey(e) {
				return true
			}
		}
	case []any:
		for _, e := range c {
			if holdsUnstableKey(e) {
				return true
			}
		}
	}
	return false
}

// rankKeys copies v, giving the string and NaN keys of each mapping that holds
// an unstable key a rank the encoder's comparison cannot get wrong. Any other
// key sorts ahead of them as it always did, and any other mapping keeps its
// keys and the encoder's order.
func rankKeys(v any) any {
	switch c := v.(type) {
	case map[string]any:
		m := make(map[any]any, len(c))
		for k, e := range c {
			m[k] = e
		}
		return rankMapping(m)
	case map[any]any:
		return rankMapping(c)
	case []any:
		out := make([]any, len(c))
		for i, e := range c {
			out[i] = rankKeys(e)
		}
		return out
	}
	return v
}

func rankMapping(m map[any]any) map[any]any {
	type pair struct {
		text string
		nan  bool
		val  any
	}
	out := make(map[any]any, len(m))
	var pairs []pair
	unstable := false
	for k, e := range m {
		e = rankKeys(e)
		unstable = unstable || unstableKey(k)
		switch k := k.(type) {
		case string:
			pairs = append(pairs, pair{text: k, val: e})
			continue
		case float64:
			if math.IsNaN(k) { // two NaN keys differ only by their value
				b, _ := yaml.Marshal(e)
				pairs = append(pairs, pair{text: ".nan " + string(b), nan: true, val: e})
				continue
			}
		}
		out[k] = e
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].text < pairs[j].text })
	for i, p := range pairs {
		switch {
		case !unstable:
			out[p.text] = p.val
		case p.nan:
			out[rankedNaN(fmt.Sprintf("%012d", i))] = p.val
		default:
			out[rankedKey(fmt.Sprintf("%012d\x00%s", i, p.text))] = p.val
		}
	}
	return out
}

// rankedKey is a key the encoder sorts by the fixed-width rank ahead of the NUL
// and writes as the text behind it, down the path any string key takes. A
// yaml.Node mapping would also encode in order, but building one reads the
// encoder's output back, which refuses a multi-line string starting with a tab,
// empties "\n", and turns a `<<` key into `!!merge <<`.
type rankedKey string

func (k rankedKey) MarshalYAML() (any, error) {
	_, text, _ := strings.Cut(string(k), "\x00")
	return text, nil
}

// rankedNaN is rankedKey for a NaN key.
type rankedNaN string

func (rankedNaN) MarshalYAML() (any, error) { return math.NaN(), nil }

// jsonKeyed rewrites value so every mapping has string keys, which is what a
// JSON object requires: a member name must be a string, and a YAML key need
// not be. yaml.v4 decodes a mapping with any non-string key as map[any]any
// rather than map[string]any, and the encoder refused it outright — a bool,
// null or float key failed the whole render with the encoder's own wording and
// the exit code for a path that didn't resolve, though the path had resolved
// and only the rendering could not be done (#239).
//
// The key's text is query.KeyText, the spelling a path expression addresses it
// by, so a member name read back out of the JSON still names the key it came
// from — `null`, not `<nil>` and not an empty string. An integer key already
// came out this way; the other scalar kinds simply never reached it.
//
// Two distinct keys can share that text: yaml.v4 rejects `1:` beside `"1":`
// as a duplicate, but not the int 1 beside the float 1.0, which decode as
// distinct keys that KeyText spells alike. One JSON object cannot hold both,
// and writing them into one map kept whichever map iteration reached last,
// so the answer changed from run to run (#300). jsonKeyed reports the first
// such collision it meets in sorted key order instead, in the same pass that
// builds the result
// — collided is true exactly when the returned value must be discarded and
// render refused.
func jsonKeyed(value any) (out any, name string, collided bool) {
	switch v := value.(type) {
	case map[any]any:
		m := make(map[string]any, len(v))
		for _, e := range query.SortedAnyEntries(v) {
			key := query.KeyText(e.Key)
			if _, dup := m[key]; dup {
				return nil, key, true
			}
			sub, name, collided := jsonKeyed(e.Val)
			if collided {
				return nil, name, true
			}
			m[key] = sub
		}
		return m, "", false
	case map[string]any:
		m := make(map[string]any, len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			sub, name, collided := jsonKeyed(v[k])
			if collided {
				return nil, name, true
			}
			m[k] = sub
		}
		return m, "", false
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			sub, name, collided := jsonKeyed(val)
			if collided {
				return nil, name, true
			}
			out[i] = sub
		}
		return out, "", false
	default:
		return value, "", false
	}
}

// checkYAMLKeys refuses a value whose mapping the encoder would write as YAML
// that can't be read back. It writes each key in its own canonical form, so keys
// distinct in the source can come out with one text (`1` and `1.0`, `"true"` and
// `True`), which yaml.v4 refuses, or one value, which rejectEqualKeys refuses
// (#385).
func checkYAMLKeys(value any) error {
	switch v := value.(type) {
	case map[any]any:
		entries := query.SortedAnyEntries(v)
		texts := make(map[string]bool, len(entries))
		decoded := make(map[any]bool, len(entries))
		for _, e := range entries {
			text, val := writtenKey(e.Key)
			if texts[text] || decoded[val] {
				return usageErr(fmt.Errorf("-o yaml: two keys in one mapping are both written %q, which YAML cannot read back; rename one of them", text))
			}
			texts[text], decoded[val] = true, true
		}
		for _, e := range entries {
			if err := checkYAMLKeys(e.Val); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			if err := checkYAMLKeys(v[k]); err != nil {
				return err
			}
		}
	case []any:
		for _, e := range v {
			if err := checkYAMLKeys(e); err != nil {
				return err
			}
		}
	}
	return nil
}

// writtenKey returns the text the encoder writes k as and what that text reads
// back as. A string's quoting doesn't count: yaml.v4 compares keys by text alone.
func writtenKey(k any) (text string, value any) {
	if s, ok := k.(string); ok {
		return s, s
	}
	b, _ := yaml.Marshal(k)
	_ = yaml.Unmarshal(b, &value)
	return strings.TrimSpace(string(b)), value
}

// rawText is value's raw (unquoted) textual representation, the same one
// renderRaw prints, minus its trailing newline.
func rawText(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case bool, int, int64, uint64, float64:
		return fmt.Sprint(v), nil
	default:
		// Fall back to compact YAML for maps and lists.
		if err := checkYAMLKeys(v); err != nil {
			return "", err
		}
		b, err := yaml.Marshal(stableKeys(v))
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(b), "\n"), nil
	}
}

func renderRaw(w io.Writer, value any) error {
	s, err := rawText(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, s)
	return err
}

// resultWriter emits a command's results either through render (one write per
// result, newline by default) or, when print0 is set, buffered as raw text
// joined with NUL — no separator after the last item, for xargs -0. print0
// only makes sense with raw output; callers are expected to have already
// forced format to "raw" (or rejected the combination) before constructing
// one, which is resolveOutputFormat's job.
type resultWriter struct {
	out    io.Writer
	format string
	print0 bool
	parts  []string
}

func (rw *resultWriter) emit(v any) error {
	if !rw.print0 {
		return render(rw.out, v, rw.format)
	}
	s, err := rawText(v)
	if err != nil {
		return err
	}
	rw.parts = append(rw.parts, s)
	return nil
}

// flush writes the buffered NUL-joined output; a no-op unless print0 is set.
func (rw *resultWriter) flush() error {
	if !rw.print0 {
		return nil
	}
	_, err := io.WriteString(rw.out, strings.Join(rw.parts, "\x00"))
	return err
}
