package main

import (
	"os"
)

func main() {
	if err := run(os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}
