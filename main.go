package main

import (
	"fmt"
	"os"

	"github.com/sushichan044/github-review-loop/cmd"
)

func main() {
	if err := cmd.Execute(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
