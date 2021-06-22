// +build testrunmain

package main

import (
	"os"
	"testing"
)

func TestRunMain(t *testing.T) {
	stripTestArgs()
	main()
}

func stripTestArgs() {
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			break
		}
	}
}
