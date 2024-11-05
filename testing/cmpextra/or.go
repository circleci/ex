package cmpextra

import (
	"fmt"

	"gotest.tools/v3/assert/cmp"
)

func Or(compares ...cmp.Comparison) cmp.Comparison {
	return func() cmp.Result {
		if len(compares) < 2 {
			return cmp.ResultFailure("Invalid Or comparison. At least 2 comparison required")
		}

		var fails []cmp.Result
		for _, compare := range compares {
			res := compare()
			if res.Success() {
				return res
			}
			fails = append(fails, res)
		}
		msg := "no comparisons passed:\n"
		for _, fail := range fails {
			if sr, ok := fail.(CompareResult); ok {
				msg += fmt.Sprintf("%s\n", sr.FailureMessage())
			} else {
				msg += fmt.Sprintf("%v\n", fail)
			}
		}
		return cmp.ResultFailure(msg)
	}
}

type CompareResult interface {
	Success() bool
	FailureMessage() string
}
