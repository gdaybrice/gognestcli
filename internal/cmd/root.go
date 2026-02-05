package cmd

import (
	"fmt"

	"github.com/alecthomas/kong"
)

var version = "dev"

type CLI struct {
	Auth     AuthCmd     `cmd:"" help:"Authenticate with Google Nest"`
	Devices  DevicesCmd  `cmd:"" help:"List Nest devices"`
	Info     InfoCmd     `cmd:"" help:"Show camera details"`
	Snapshot SnapshotCmd `cmd:"" help:"Take a camera snapshot"`
	Record   RecordCmd   `cmd:"" help:"Record a video clip"`
	Events   EventsCmd   `cmd:"" help:"Listen for motion/person events"`
	Version  VersionCmd  `cmd:"" help:"Print version"`
}

type VersionCmd struct{}

func (v *VersionCmd) Run() error {
	fmt.Println("gognestcli", version)
	return nil
}

func Execute() int {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("gognestcli"),
		kong.Description("CLI for Google Nest cameras via the Smart Device Management API"),
		kong.UsageOnError(),
	)
	if err := ctx.Run(); err != nil {
		fmt.Fprintf(ctx.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}
