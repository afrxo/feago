// internal/color.go
package internal

import (
	"os"
	"strings"
)

const (
	ansiReset  = "\033[0m"
	codeBold   = "1"
	codeDim    = "2"
	codeRed    = "31"
	codeGreen  = "32"
	codeYellow = "33"
	codeBlue   = "34"
)

var colorEnabled = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func Style(s string, codes ...string) string {
	if !colorEnabled || len(codes) == 0 {
		return s
	}
	return "\033[" + strings.Join(codes, ";") + "m" + s + ansiReset
}

func Bold(s string) string       { return Style(s, codeBold) }
func Dim(s string) string        { return Style(s, codeDim) }
func Red(s string) string        { return Style(s, codeRed) }
func Green(s string) string      { return Style(s, codeGreen) }
func Yellow(s string) string     { return Style(s, codeYellow) }
func Blue(s string) string       { return Style(s, codeBlue) }
func BoldYellow(s string) string { return Style(s, codeBold, codeYellow) }
func BoldRed(s string) string    { return Style(s, codeBold, codeRed) }

const (
	SymOK    = "✓"
	SymErr   = "✗"
	SymWarn  = "⚠"
	SymInfo  = "→"
	SymCycle = "↻"
	SymDot   = "·"
)
