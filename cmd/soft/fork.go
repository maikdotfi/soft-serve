package main

import (
	cicmd "github.com/charmbracelet/soft-serve/cmd/soft/ci"
	"github.com/charmbracelet/soft-serve/cmd/soft/restore"
	"github.com/spf13/cobra"
)

// forkCommands are the subcommands this fork adds to the soft binary.
var forkCommands = []*cobra.Command{
	restore.Command,
	cicmd.Command,
}
