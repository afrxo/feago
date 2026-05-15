// internal/commands.go
package internal

import (
	"fmt"
	"os"
	"sort"
)

type Command struct {
	Name    string
	Aliases []string
	Summary string
	Usage   string
	Run     func(flags map[string]string, values []string) error
}

var Commands []Command

func init() {
	Commands = []Command{
		{
			Name:    "init",
			Summary: "Initializes a new feature-based feago project",
			Usage: `feago init [dir] [--force]

Initializes a new feature-based feago project in dir (default: current
directory). Existing files are preserved unless --force is set.

OPTIONS:
    --force    Overwrite existing files`,
			Run: InitCommand,
		},
		{
			Name:    "build",
			Summary: "Generates a Rojo project file from feature folders",
			Usage: `feago build [sourceDir] [--project <file>]

Reads sourceDir (default: src/) and writes the Rojo project file.

OPTIONS:
    --project <file>    Project file to write (default: default.project.json)`,
			Run: BuildCommand,
		},
		{
			Name:    "watch",
			Aliases: []string{"serve"},
			Summary: "Builds the project, then rebuilds on source changes",
			Usage: `feago watch [sourceDir] [--project <file>]

Builds the project once, then watches sourceDir for file changes and
rebuilds the project file on every change. Stop with Ctrl+C.

OPTIONS:
    --project <file>    Project file to write (default: default.project.json)`,
			Run: WatchCommand,
		},
		{
			Name:    "version",
			Summary: "Prints version information",
			Usage:   `feago version    (alias: -V, --version)`,
			Run:     VersionCommand,
		},
		{
			Name:    "help",
			Summary: "Prints this message or the help of the given subcommand",
			Usage:   `feago help [command]`,
			Run:     HelpCommand,
		},
	}
}

func findCommand(name string) *Command {
	for i := range Commands {
		if Commands[i].Name == name {
			return &Commands[i]
		}
		for _, alias := range Commands[i].Aliases {
			if alias == name {
				return &Commands[i]
			}
		}
	}
	return nil
}

func HelpCommand(flags map[string]string, values []string) error {
	if len(values) > 0 {
		name := values[0]
		if cmd := findCommand(name); cmd != nil {
			fmt.Fprintln(os.Stdout, cmd.Summary)
			fmt.Fprintln(os.Stdout)
			fmt.Fprintln(os.Stdout, cmd.Usage)
			return nil
		}
		fmt.Fprintln(os.Stderr, BoldRed(SymErr+" unknown command:"), name)
		fmt.Fprintln(os.Stderr)
	}

	fmt.Fprintln(os.Stdout, BoldYellow("feago"), Yellow(Version))
	fmt.Fprintln(os.Stdout, "Generates Rojo projects from feature-organized source folders.")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, Bold("USAGE:"))
	fmt.Fprintln(os.Stdout, "    feago [OPTIONS] <SUBCOMMAND>")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, Bold("OPTIONS:"))
	fmt.Fprintln(os.Stdout, "    -h, --help       Print help information")
	fmt.Fprintln(os.Stdout, "    -V, --version    Print version information")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, Bold("SUBCOMMANDS:"))

	names := make([]string, 0, len(Commands))
	width := 0
	for _, c := range Commands {
		names = append(names, c.Name)
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	sort.Strings(names)

	for _, n := range names {
		c := findCommand(n)
		fmt.Fprintf(os.Stdout, "    %s    %s\n", Green(padRight(c.Name, width)), c.Summary)
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, Dim("Run `feago help <subcommand>` for more information on a specific subcommand."))
	return nil
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
