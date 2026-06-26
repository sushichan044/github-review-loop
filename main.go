package main

import (
	"fmt"
	"os"

	"github.com/sushichan044/mergeable-please/cmd"
)

func main() {
	if err := cmd.Execute(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
