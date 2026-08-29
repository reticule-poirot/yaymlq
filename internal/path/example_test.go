package path_test

import (
	"fmt"

	"github.com/reticule-poirot/yaymlq/internal/path"
)

func ExampleParse() {
	segs, _ := path.Parse(`services[0]."odd.key".*`)
	for _, s := range segs {
		fmt.Printf("%q\n", s.String())
	}
	// Output:
	// "services"
	// "[0]"
	// "odd.key"
	// "*"
}

func ExampleFormat() {
	segs, _ := path.Parse("a.b.0.c")
	fmt.Println(path.Format(segs))
	// Output: a.b[0].c
}
