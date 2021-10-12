package main

import (
	"testing"

	"github.com/circleci/ex/testing/kongtest"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

func TestHelp(t *testing.T) {
	s := kongtest.Help(t, &cli{})
	assert.Check(t, golden.String(s, "help.txt"))
}
