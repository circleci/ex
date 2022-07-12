/*
Package kongtest helps write golden tests for your [Kong](https://github.com/alecthomas/kong)
CLI parsing.
*/
package kongtest

import (
	"bytes"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/v3/assert"
)

func Help(t *testing.T, cli interface{}) string {
	w := bytes.NewBuffer(nil)
	rc := -1
	app, err := kong.New(cli,
		kong.Name("test-app"),
		kong.Writers(w, w),
		kong.Exit(func(i int) {
			rc = i
		}),
	)
	assert.Check(t, err)

	// Intentionally ignore the error, as it's not useful
	_, _ = app.Parse([]string{"--help"})
	assert.Check(t, cmp.Equal(0, rc))

	return w.String()
}
