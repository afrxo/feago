// internal/version.go
package internal

import (
	"fmt"
	"os"
)

// version is set at build like this:
// go build -ldflags "-X github.com/afrxo/feago/internal.Version=X.X.X"
var Version = "dev"

func VersionCommand(flags map[string]string, values []string) error {
	fmt.Fprintln(os.Stdout, BoldYellow("feago"), Yellow(Version))
	return nil
}
