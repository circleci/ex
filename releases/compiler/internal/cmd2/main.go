package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Printf("command 2: %v %s\n", os.Args[1:], importantString())
}
