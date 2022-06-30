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

	_, err = app.Parse([]string{"--help"})
	assert.Check(t, err)
	assert.Check(t, cmp.Equal(0, rc))

	return w.String()
}
