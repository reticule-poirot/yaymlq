package cmd

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"go.yaml.in/yaml/v4"
)

// YAML 1.2.2 §6.8.1 has a processor accept "%YAML 1.2" and "%YAML 1.1", read a
// higher minor version with a warning, and reject a higher major one. yaml.v4
// refuses everything but 1.1, with no option to do otherwise, and uses the version
// for nothing else while reading. So the parser is shown a 1.x directive as 1.1,
// in a copy of the same length, which leaves every offset and line number where
// the splice expects them. Everything written comes from the source, so the file
// keeps what it said.

// versionDirective is one "%YAML" line in directive position.
type versionDirective struct {
	line         int // 1-based
	major, minor int
	version      string // as spelled
	// minorStart and minorEnd are the bytes of the minor version in the source.
	minorStart, minorEnd int
}

// readable reports whether this program can read the version: 1.1 or any later
// 1.x. The spec says nothing of what comes before 1.1.
func (d versionDirective) readable() bool { return d.major == 1 && d.minor >= 1 }

var versionDirectiveLine = regexp.MustCompile(`^%YAML[ \t]+([0-9]+)\.([0-9]+)(?:[ \t]+(?:#.*)?)?$`)

// versionDirectives finds the "%YAML" lines of a stream that really are
// directives. A line is one only at the start of the stream or after "...",
// through comments, blanks and other directives (§9.1.2, [209], [211]). Past a
// "---" the same text is content, and rewriting it would change a value.
func versionDirectives(data []byte) []versionDirective {
	if !bytes.Contains(data, []byte("%YAML")) {
		return nil
	}
	var out []versionDirective
	inPrefix := true
	lineNo := 0
	for i := 0; i <= len(data); {
		start := i
		for i < len(data) && yamlBreakWidth(data[i:]) == 0 {
			i++
		}
		end := i
		if i < len(data) {
			i += yamlBreakWidth(data[i:])
		} else {
			i++
		}
		lineNo++
		text := data[start:end]
		if bytes.HasPrefix(text, []byte(utf8BOM)) {
			start += len(utf8BOM)
			text = data[start:end]
		}
		s := string(text)
		switch {
		case isDocEnd(s):
			inPrefix = true
		case !inPrefix:
		case isDocStart(s):
			inPrefix = false
		case len(bytes.TrimLeft(text, " \t")) == 0, text[0] == '#':
		case text[0] == '%':
			if m := versionDirectiveLine.FindSubmatchIndex(text); m != nil {
				major, majorErr := strconv.Atoi(string(text[m[2]:m[3]]))
				minor, minorErr := strconv.Atoi(string(text[m[4]:m[5]]))
				if majorErr == nil && minorErr == nil {
					out = append(out, versionDirective{
						line: lineNo, major: major, minor: minor,
						version:    string(text[m[2]:m[5]]),
						minorStart: start + m[4], minorEnd: start + m[5],
					})
				}
			}
		default:
			inPrefix = false
		}
	}
	return out
}

// parserInput is data as the parser should see it: every readable directive
// spelled 1.1. Input with nothing to change is returned as it came.
func parserInput(data []byte) []byte {
	src := toUTF8(data)
	var masked []byte
	for _, d := range versionDirectives(src) {
		if !d.readable() || d.minor == 1 {
			continue
		}
		if masked == nil {
			masked = bytes.Clone(src)
		}
		masked[d.minorStart] = '1'
		for i := d.minorStart + 1; i < d.minorEnd; i++ {
			masked[i] = ' '
		}
	}
	if masked == nil {
		return data
	}
	return masked
}

// newDecoder is the one way a stream reaches the parser.
func newDecoder(data []byte) *yaml.Decoder {
	return yaml.NewDecoder(bytes.NewReader(parserInput(data)))
}

// unreadableVersionDirective finds the first directive naming a version this
// program can't read.
func unreadableVersionDirective(data []byte) (versionDirective, bool) {
	for _, d := range versionDirectives(toUTF8(data)) {
		if !d.readable() {
			return d, true
		}
	}
	return versionDirective{}, false
}

// noteNewerYAML writes §6.8.1's warning for a minor version past 1.2, once however
// many documents have one. On stderr, so stdout stays what the run found.
func noteNewerYAML(w io.Writer, prefix string, data []byte) {
	for _, d := range versionDirectives(toUTF8(data)) {
		if d.major == 1 && d.minor > 2 {
			_, _ = fmt.Fprintf(w, "%snote: line %d: %%YAML %s is newer than YAML 1.2, the version this reads it as\n", prefix, d.line, d.version)
			return
		}
	}
}
