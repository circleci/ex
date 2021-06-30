package kongtest

import (
	"bytes"
	"testing"

	"github.com/alecthomas/kong"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func Help(t *testing.T, cli interface{}) string {
	w := bytes.NewBuffer(nil)
	exited := false
	app, err := kong.New(cli,
		kong.Name("test-app"),
		kong.Writers(w, w),
		kong.Exit(func(int) {
			exited = true
			panic("value for panic") // Panic to fake "exit".
		}),
	)
	assert.Check(t, err)

	var value interface{}
	func() {
		defer func() {
			value = recover()
		}()
		_, err := app.Parse([]string{"--help"})
		assert.Check(t, err)
	}()
	assert.Check(t, cmp.Equal(value, "value for panic"))
	assert.Check(t, exited)

	return w.String()
}
