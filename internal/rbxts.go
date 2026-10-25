// internal/rbxts.go
package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// a roblox-ts project, read from tsconfig.json. rbxtsc compiles rootDir into
// outDir and Rojo serves outDir, so feago reads sources from rootDir but
// points every $path at the compiled file in outDir.
type rbxtsProject struct {
	RootDir string
	OutDir  string
	LuauExt string
}

type tsconfig struct {
	CompilerOptions struct {
		RootDir string `json:"rootDir"`
		OutDir  string `json:"outDir"`
	} `json:"compilerOptions"`
	Rbxts struct {
		Luau *bool `json:"luau"`
	} `json:"rbxts"`
}

// returns nil (and no error) when dir has no tsconfig.json, i.e. plain Luau.
func loadRbxtsProject(dir string) (*rbxtsProject, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "tsconfig.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg tsconfig
	if err := json.Unmarshal(stripJSONC(raw), &cfg); err != nil {
		return nil, fmt.Errorf("Invalid tsconfig.json: %v", err)
	}

	// rbxtsc requires both, these are just its template defaults
	rootDir := cfg.CompilerOptions.RootDir
	if rootDir == "" {
		rootDir = "src"
	}
	outDir := cfg.CompilerOptions.OutDir
	if outDir == "" {
		outDir = "out"
	}
	ext := ".luau"
	if cfg.Rbxts.Luau != nil && !*cfg.Rbxts.Luau {
		ext = ".lua"
	}

	return &rbxtsProject{
		RootDir: resolveFrom(dir, rootDir),
		OutDir:  resolveFrom(dir, outDir),
		LuauExt: ext,
	}, nil
}

func resolveFrom(dir, p string) string {
	p = filepath.FromSlash(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}

// mirrors rbxtsc's PathTranslator.getOutputPath:
// src/a/index.server.ts -> out/a/init.server.luau. Everything else
// (.luau files) is copied to outDir as-is.
func (p *rbxtsProject) outputPath(path string) string {
	rel, err := filepath.Rel(p.RootDir, path)
	if err != nil {
		return path
	}
	dir, base := filepath.Split(rel)
	if isCompiledTS(base) {
		// rbxtsc splits on the first dot: index.server.ts -> index + .server
		name, subExts, _ := strings.Cut(strings.TrimSuffix(base, filepath.Ext(base)), ".")
		if name == "index" {
			name = "init"
		}
		if subExts != "" {
			name += "." + subExts
		}
		base = name + p.LuauExt
	}
	return filepath.Join(p.OutDir, dir, base)
}

func isWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// .ts/.tsx files rbxtsc emits Luau for; declaration files emit nothing.
func isCompiledTS(base string) bool {
	ext := filepath.Ext(base)
	if ext != ".ts" && ext != ".tsx" {
		return false
	}
	return !strings.HasSuffix(strings.TrimSuffix(base, ext), ".d")
}

// tsconfig.json is JSONC: strips comments and trailing commas so
// encoding/json can read it.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false

	for i := 0; i < len(data); i++ {
		c := data[i]

		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(data) {
				i++
				out = append(out, data[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}

		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			i--
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++
		case c == ']' || c == '}':
			// drop a trailing comma before the closer
			j := len(out) - 1
			for j >= 0 && isJSONSpace(out[j]) {
				j--
			}
			if j >= 0 && out[j] == ',' {
				out = append(out[:j], out[j+1:]...)
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
