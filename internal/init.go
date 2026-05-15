// internal/init.go
package internal

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed all:templates
var templatesFS embed.FS

func InitCommand(flags map[string]string, values []string) error {
	targetDir := "."
	if len(values) > 0 {
		targetDir = values[0]
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absTarget, 0755); err != nil {
		return err
	}

	force := flags["force"] == "true"
	projectCreated := false
	created := 0

	name := filepath.Base(absTarget)
	displayTarget := targetDir
	if displayTarget == "." {
		displayTarget = name
	}
	fmt.Fprintf(os.Stdout, "%s %s %s\n", BoldYellow("feago"), Yellow(Version), Dim(SymDot+" Initializing project"))
	fmt.Fprintf(os.Stdout, "%s %s\n", Blue(SymInfo), displayTarget)

	err = fs.WalkDir(templatesFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "templates" {
			return nil
		}

		rel := strings.TrimPrefix(p, "templates/")
		dst := filepath.Join(absTarget, filepath.FromSlash(rel))

		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}

		if _, err := os.Stat(dst); err == nil && !force {
			fmt.Fprintf(os.Stdout, "  %s %s %s\n", Dim(SymDot), rel, Dim("(Exists)"))
			return nil
		}

		data, err := fs.ReadFile(templatesFS, path.Clean(p))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return err
		}
		if rel == "default.project.json" {
			projectCreated = true
		}
		created++
		fmt.Fprintf(os.Stdout, "  %s %s\n", Dim(SymOK), rel)
		return nil
	})

	if err != nil {
		return err
	}

	if projectCreated {
		projectPath := filepath.Join(absTarget, "default.project.json")
		raw, err := os.ReadFile(projectPath)
		if err != nil {
			return err
		}
		var pf RojoProjectFile
		if err := json.Unmarshal(raw, &pf); err != nil {
			return err
		}
		pf.Name = filepath.Base(absTarget)
		out, err := json.MarshalIndent(pf, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(projectPath, append(out, '\n'), 0644); err != nil {
			return err
		}
	}

	res, err := Build(absTarget, "src", "default.project.json", true)
	if err != nil {
		return err
	}

	featuresLabel := "feature"
	if len(res.Features) > 1 {
		featuresLabel = "features"
	}
	count := Dim(fmt.Sprintf("%s %d files %s %d %s", SymDot, created, SymDot, len(res.Features), featuresLabel))
	fmt.Fprintf(os.Stdout, "%s %s\n\n", Green(SymOK+" Initialized"), count)
	fmt.Fprintf(os.Stdout, "%s\n", Dim("■ Run `feago build` to regenerate"))
	fmt.Fprintf(os.Stdout, "%s\n", Dim("■ Run `feago watch` for live sync"))
	return nil
}
