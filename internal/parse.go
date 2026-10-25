// internal/parse.go
package internal

import "strings"

// flags that take a value; every other flag is a boolean switch, so
// `feago init --ts game` keeps `game` as a positional value.
var valueFlags = map[string]bool{
	"project": true,
}

func Parse(args []string) (map[string]string, []string) {
	flags := make(map[string]string)
	values := make([]string, 0)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if len(arg) == 0 {
			continue
		}

		if arg[0] == '-' {
			key := strings.TrimLeft(arg, "-")

			if valueFlags[key] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags[key] = args[i+1]
				i++
				continue
			}

			bool := "true"
			if strings.HasPrefix(arg, "--no-") {
				bool = "false"
				key = key[3:]
			}
			flags[key] = bool
		} else {
			values = append(values, arg)
		}
	}

	return flags, values
}
