package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s GIT_ROOT < DIFF\n", os.Args[0])
}

func main() {
	if len(os.Args) != 2 {
		usage()
		os.Exit(2)
	}

	if err := run(os.Args[1], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "diffwhat: %v\n", err)
		os.Exit(1)
	}
}
