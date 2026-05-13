// internal/parse.go
package internal

import "strings"

func Parse(args []string) (map[string]string, []string) {
	flags := make(map[string]string)
	values := make([]string, 0)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if len(arg) == 0 {
			continue
		}

		if arg[0] == '-' {
			var next string

			if i+1 < len(args) {
				next = args[i+1]
			}

			if len(next) != 0 && next[:2] != "--" && next[0] != '-' {
				flags[arg[2:]] = next
				i++
			} else {
				bool := "true"
				key := strings.Trim(arg, "-")
				if len(arg) >= 4 && arg[:4] == "--no" {
					bool = "false"
					key = key[3:]
				}
				flags[key] = bool
			}
		} else {
			values = append(values, arg)
		}
	}

	return flags, values
}
