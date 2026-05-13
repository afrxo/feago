package main

import (
	"github.com/afrxo/feago/internal"
	"fmt"
	"log"
	"os"
)

func main() {
	log.SetFlags(0)
	flags, values := internal.Parse(os.Args[1:])

	if flags["V"] == "true" || flags["version"] == "true" {
		internal.VersionCommand(flags, nil)
		return
	}

	wantsHelp := flags["help"] == "true" || flags["h"] == "true"

	if len(values) == 0 || wantsHelp && len(values) == 0 {
		internal.HelpCommand(flags, values)
		return
	}

	name := values[0]
	values = values[1:]

	if wantsHelp {
		internal.HelpCommand(flags, []string{name})
		return
	}

	cmd := findCommand(name)
	if cmd == nil {
		fmt.Fprintln(os.Stderr, internal.BoldRed("Unknown command:"), name)
		fmt.Fprintln(os.Stderr)
		internal.HelpCommand(flags, nil)
		os.Exit(1)
	}

	if err := cmd.Run(flags, values); err != nil {
		fmt.Fprintln(os.Stderr, internal.BoldRed("error:"), err)
		os.Exit(1)
	}
}

func findCommand(name string) *internal.Command {
	for i := range internal.Commands {
		if internal.Commands[i].Name == name {
			return &internal.Commands[i]
		}
	}
	return nil
}
