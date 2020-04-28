package honeycomb

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/circleci/backplane-go/envconfig"
)

// Config currently is set up to support envconfig, with the 'config' annotation,
// this is a dependency that would be better removed from this package in the future.
type Config struct {
	HoneycombEnabled bool                  `config:"honeycomb,help=Send traces to honeycomb."`
	HoneycombDataset string                `config:"honeycomb_dataset"`
	HoneycombKey     *envconfig.SecretFile `config:"honeycomb_key"`
	SampleTraces     bool                  `config:"sample_traces"`
	Format           *Format               `config:"format,help=Format used for stderr logging"`
	Host             string                `config:"-"`
}

func (c *Config) Validate() error {
	if c.HoneycombEnabled && c.HoneycombKey.Value() == "" {
		return errors.New("honeycomb_key key required for honeycomb")
	}
	return nil
}

type Format struct {
	w io.Writer
}

func (f *Format) FromConfig(raw interface{}) error {
	value, ok := raw.(string)
	if !ok {
		return fmt.Errorf("format must be a string, not %T", raw)
	}
	switch value {
	case "json":
		f.w = os.Stderr
	case "text":
		f.w = DefaultTextFormat
	case "colour", "color":
		f.w = ColourTextFormat
	default:
		return fmt.Errorf("unknown format: %s", value)
	}
	return nil
}

func (f *Format) Value() io.Writer {
	if f.w == nil {
		panic("No o11y format selected")
	}
	return f.w
}
