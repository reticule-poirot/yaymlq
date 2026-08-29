package query_test

import (
	"fmt"

	"github.com/reticule-poirot/yaymlq/internal/query"
	"gopkg.in/yaml.v3"
)

func ExampleRun() {
	var doc any
	_ = yaml.Unmarshal([]byte(`
services:
  web:
    image: nginx:1.27
  db:
    image: postgres:16
`), &doc)

	got, _ := query.Run(doc, "services.web.image")
	fmt.Println(got[0])
	// Output: nginx:1.27
}

func ExampleRun_wildcard() {
	var doc any
	_ = yaml.Unmarshal([]byte(`
services:
  web: {image: nginx:1.27}
  db: {image: postgres:16}
`), &doc)

	// Map values are visited in key order.
	got, _ := query.Run(doc, "services.*.image")
	fmt.Println(got)
	// Output: [postgres:16 nginx:1.27]
}
