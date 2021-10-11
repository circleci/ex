package httprecorder

import (
	gocmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func IgnoreHeaders(headers ...string) gocmp.Option {
	return cmpopts.IgnoreMapEntries(func(h string, _ []string) bool {
		for _, header := range headers {
			if header == h {
				return true
			}
		}
		return false
	})
}

func OnlyHeaders(headers ...string) gocmp.Option {
	return cmpopts.IgnoreMapEntries(func(h string, _ []string) bool {
		for _, header := range headers {
			if header == h {
				return false
			}
		}
		return true
	})
}
