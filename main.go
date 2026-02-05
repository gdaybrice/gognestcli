package main

import (
	"os"

	"github.com/brice/gognestcli/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
