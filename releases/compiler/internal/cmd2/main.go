package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Printf("command 2: %v\n", os.Args[1:])
}
