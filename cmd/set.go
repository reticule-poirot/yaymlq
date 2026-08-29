package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/reticule-poirot/yaymlq/internal/path"
	"github.com/reticule-poirot/yaymlq/internal/ymledit"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type setOptions struct {
	inPlace  bool
	asString bool
	docIdx   int
	maxBytes int64
}

func newSetCommand() *cobra.Command {
	opts := &setOptions{maxBytes: defaultMaxBytes}

	cmd := &cobra.Command{
		Use:   "set <path> <value> [file]",
		Short: "Set the value at a path, preserving comments and formatting",
		Long: "Set writes a new value at the given path and prints the whole document.\n" +
			"Missing intermediate mapping keys are created. Wildcards are not allowed.\n" +
			"The value is parsed as YAML (so 8080 is an int, true a bool); use --string\n" +
			"to keep it a string. With --in-place the file is rewritten instead of printed.",
		Example: "  yaymlq set '.services.web.image' nginx:1.28 compose.yml\n" +
			"  yaymlq set --string '.metadata.annotations.\"team\"' platform k8s.yaml\n" +
			"  cat cfg.yaml | yaymlq set .debug true",
		Args:         cobra.RangeArgs(2, 3),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return runSet(c, opts, args)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.inPlace, "in-place", "i", false, "rewrite the file instead of printing to stdout")
	f.BoolVarP(&opts.asString, "string", "s", false, "treat <value> as a string, not parsed YAML")
	f.IntVar(&opts.docIdx, "doc", 0, "index of the document to edit in a multi-doc stream")
	f.Int64Var(&opts.maxBytes, "max-bytes", opts.maxBytes, "max input bytes to buffer; 0 = unlimited")

	return cmd
}

func runSet(c *cobra.Command, opts *setOptions, args []string) error {
	expr, rawValue := args[0], args[1]
	filename := ""
	if len(args) == 3 && args[2] != "-" {
		filename = args[2]
	}

	if opts.inPlace && filename == "" {
		return errors.New("--in-place needs a file argument")
	}

	segs, err := path.Parse(expr)
	if err != nil {
		return err
	}

	var src io.Reader = c.InOrStdin()
	if filename != "" {
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer file.Close()
		src = file
	}

	data, err := readCapped(src, opts.maxBytes)
	if err != nil {
		return err
	}

	docs, err := decodeNodes(data)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("no YAML documents on input")
	}
	if opts.docIdx < 0 || opts.docIdx >= len(docs) {
		return fmt.Errorf("document index %d out of range (%d documents)", opts.docIdx, len(docs))
	}

	value, err := ymledit.ParseValue(rawValue, opts.asString)
	if err != nil {
		return fmt.Errorf("parsing value: %w", err)
	}

	if err := ymledit.Set(docs[opts.docIdx], segs, value); err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return err
		}
	}
	if err := enc.Close(); err != nil {
		return err
	}

	if opts.inPlace {
		info, statErr := os.Stat(filename)
		perm := os.FileMode(0o644)
		if statErr == nil {
			perm = info.Mode().Perm()
		}
		return os.WriteFile(filename, buf.Bytes(), perm)
	}

	_, err = c.OutOrStdout().Write(buf.Bytes())
	return err
}

func decodeNodes(data []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}
		docs = append(docs, &n)
	}
	return docs, nil
}
