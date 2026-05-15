// internal/build.go
package internal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var directiveRe = regexp.MustCompile(`^--@load:(server|client|preload|shared)\s*$`)

const directiveScanLimit = 4096

var validRealms = map[string]string{
	"server":  "Server",
	"client":  "Client",
	"preload": "Preload",
	"shared":  "Shared",
}

type RojoProjectFile struct {
	Name              string         `json:"name"`
	Tree              map[string]any `json:"tree"`
	EmitLegacyScripts *bool          `json:"emitLegacyScripts,omitempty"`
}

type serviceTarget struct {
	Service   string
	Subfolder string
}

var serviceTargets = map[string]serviceTarget{
	"Server":  {Service: "ServerScriptService"},
	"Client":  {Service: "ReplicatedStorage", Subfolder: "Client"},
	"Shared":  {Service: "ReplicatedStorage", Subfolder: "Shared"},
	"Preload": {Service: "ReplicatedFirst"},
}

type sourceFile struct {
	Realm    string
	Feature  string
	SubDirs  []string
	Name     string
	FullPath string
	IsInit   bool
}

type BuildResult struct {
	Files    int
	Changed  bool
	Features []string
}

func BuildCommand(flags map[string]string, values []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	rojoProjectFile := "default.project.json"
	if project, ok := flags["project"]; ok {
		rojoProjectFile = project
	}

	sourceDir := "src"
	if len(values) > 0 {
		sourceDir = values[0]
	}

	_, err = Build(wd, sourceDir, rojoProjectFile)
	return err
}

func Build(wd, sourceDir, rojoProjectFile string) (*BuildResult, error) {
	projectPath := filepath.Join(wd, rojoProjectFile)
	projectDir := filepath.Dir(projectPath)
	sourcePath := resolveSourceDir(sourceDir, wd, projectDir)

	project, raw, err := loadProject(projectPath, rojoProjectFile, wd)
	if err != nil {
		return nil, err
	}

	files, err := collectSourceFiles(sourcePath, sourceDir, projectDir)
	if err != nil {
		return nil, err
	}

	emitLegacy := false
	project.EmitLegacyScripts = &emitLegacy

	if project.Tree == nil {
		project.Tree = map[string]any{}
	}
	resetManagedSubtrees(project.Tree)
	populateTree(project.Tree, files, projectDir, sourcePath)

	changed, err := writeIfChanged(projectPath, project, raw)
	if err != nil {
		return nil, err
	}

	featureSet := map[string]struct{}{}
	for _, f := range files {
		featureSet[f.Feature] = struct{}{}
	}
	features := make([]string, 0, len(featureSet))
	for k := range featureSet {
		features = append(features, k)
	}
	sort.Strings(features)

	res := &BuildResult{Files: len(files), Changed: changed, Features: features}

	featuresLabel := "feature"
	if len(features) > 1 {
		featuresLabel = "features"
	}

	count := Dim(fmt.Sprintf("%s %d files %s %d %s", SymDot, len(files), SymDot, len(features), featuresLabel))
	if changed {
		fmt.Fprintf(os.Stdout, "%s %s %s\n", Green(SymOK+" built"), rojoProjectFile, count)
	} else {
		fmt.Fprintf(os.Stdout, "%s %s %s\n", Dim(SymDot+" unchanged"), rojoProjectFile, count)
	}
	if len(features) > 0 {
		fmt.Fprintf(os.Stdout, "  %s\n", Dim(strings.Join(features, "  "+SymDot+"  ")))
	}
	return res, nil
}

// tries wd-relative first (shell convention), then project-relative
// buuut still returns wd-relative if neither exists so the caller's "not found" error stays useful.
func resolveSourceDir(sourceDir, wd, projectDir string) string {
	if filepath.IsAbs(sourceDir) {
		return sourceDir
	}
	wdRel := filepath.Join(wd, sourceDir)
	if info, err := os.Stat(wdRel); err == nil && info.IsDir() {
		return wdRel
	}
	projRel := filepath.Join(projectDir, sourceDir)
	if info, err := os.Stat(projRel); err == nil && info.IsDir() {
		return projRel
	}
	return wdRel
}

func loadProject(projectPath, name, wd string) (*RojoProjectFile, []byte, error) {
	raw, err := os.ReadFile(projectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("project file not found: %s\n  %s", name, Dim(SymDot+" create one, or pass --project <file>"))
		}
		return nil, nil, err
	}

	project := &RojoProjectFile{}
	if err := json.Unmarshal(raw, project); err != nil {
		return nil, nil, err
	}
	return project, raw, nil
}

func collectSourceFiles(sourcePath, sourceDir, projectDir string) ([]sourceFile, error) {
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source dir not found: %s\n  %s", sourceDir, Dim(SymDot+" create it, or pass `feago build <dir>`"))
		}
		return nil, err
	}

	sidecarCache := map[string]string{}

	var files []sourceFile
	for _, entry := range entries {
		if !entry.Type().IsDir() {
			continue
		}
		feature := entry.Name()
		featurePath := filepath.Join(sourcePath, feature)

		err := filepath.WalkDir(featurePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}

			base := filepath.Base(path)
			if base == ".feago" {
				return nil
			}
			if !strings.HasSuffix(base, ".luau") {
				return nil
			}

			rel, _ := filepath.Rel(featurePath, path)
			subDirs := splitSubDirs(filepath.Dir(rel))

			folderRealm := resolveFolderRealm(filepath.Dir(path), featurePath, sidecarCache)

			realm, err := classify(path, base, folderRealm)
			if err != nil {
				return err
			}

			name := stripScriptSuffix(base)
			files = append(files, sourceFile{
				Realm:    realm,
				Feature:  feature,
				SubDirs:  subDirs,
				Name:     name,
				FullPath: path,
				IsInit:   name == "init",
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func classify(path, filename, folderRealm string) (string, error) {
	switch {
	case strings.HasSuffix(filename, ".server.luau"):
		return "Server", nil
	case strings.HasSuffix(filename, ".client.luau"):
		directive, err := scanDirective(path)
		if err != nil {
			return "", err
		}
		if directive == "preload" {
			return "Preload", nil
		}
		return "Client", nil
	}

	directive, err := scanDirective(path)
	if err != nil {
		return "", err
	}
	if directive != "" {
		return validRealms[directive], nil
	}
	if folderRealm != "" {
		return folderRealm, nil
	}
	return "Shared", nil
}

// reads up to directiveScanLimit bytes from path and looks for
// `--@load:<realm>` as the first non-blank line. Returns "" if absent or invalid.
func scanDirective(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	reader := bufio.NewReader(io.LimitReader(f, directiveScanLimit))
	for {
		line, err := reader.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if m := directiveRe.FindStringSubmatch(trimmed); m != nil {
				return m[1], nil
			}
			// stop at first non-blank line if not recognized
			if !strings.HasPrefix(trimmed, "--") {
				return "", nil
			}
		}
		if err == io.EOF {
			return "", nil
		}
		if err != nil {
			return "", err
		}
	}
}

// walks from root down to dir, returning realm from
// nearest ancestor `.feago` sidecar; uses cache too.
func resolveFolderRealm(dir, root string, cache map[string]string) string {
	if !strings.HasPrefix(dir, root) {
		return ""
	}
	if cached, ok := cache[dir]; ok {
		return cached
	}
	realm := ""
	if sidecar, ok := readSidecar(dir); ok {
		realm = sidecar
	} else if dir != root {
		parent := filepath.Dir(dir)
		realm = resolveFolderRealm(parent, root, cache)
	}
	cache[dir] = realm
	return realm
}

func readSidecar(dir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".feago"))
	if err != nil {
		return "", false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) != "realm" {
			continue
		}
		v := strings.ToLower(strings.TrimSpace(value))
		if realm, ok := validRealms[v]; ok {
			return realm, true
		}
		fmt.Fprintf(os.Stderr, "%s unknown realm %q in %s\n", Yellow(SymWarn+" warn"), v, filepath.Join(dir, ".feago"))
		return "", false
	}
	return "", false
}

func splitSubDirs(dir string) []string {
	if dir == "." || dir == "" {
		return nil
	}
	return strings.Split(dir, string(filepath.Separator))
}

func stripScriptSuffix(base string) string {
	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.TrimSuffix(name, ".client")
	name = strings.TrimSuffix(name, ".server")
	return name
}

// clears owned subtrees so populateTree can refill from src/.
func resetManagedSubtrees(tree map[string]any) {
	for _, target := range serviceTargets {
		svc, ok := tree[target.Service].(map[string]any)
		if !ok {
			continue
		}
		if target.Subfolder == "" {
			clearChildren(svc)
		} else if sub, ok := svc[target.Subfolder].(map[string]any); ok {
			clearChildren(sub)
		}
	}
}

func clearChildren(node map[string]any) {
	for key := range node {
		if !strings.HasPrefix(key, "$") {
			delete(node, key)
		}
	}
}

func populateTree(tree map[string]any, files []sourceFile, projectDir, sourcePath string) {
	initDirs := map[string]bool{}
	for _, f := range files {
		if f.IsInit {
			initDirs[filepath.Dir(f.FullPath)] = true
		}
	}

	for _, f := range files {
		if f.IsInit {
			continue
		}
		if insideInitDir(f.FullPath, sourcePath, initDirs) {
			continue
		}
		target := serviceTargets[f.Realm]

		service := folder(tree, target.Service, target.Service)
		parent := service
		if target.Subfolder != "" {
			parent = folder(service, target.Subfolder, "Folder")
		}

		featureNode := folder(parent, f.Feature, "Folder")

		current := featureNode
		for _, part := range f.SubDirs {
			current = folder(current, part, "Folder")
		}

		relPath, err := filepath.Rel(projectDir, f.FullPath)
		if err != nil {
			relPath = f.FullPath
		}
		current[f.Name] = map[string]any{"$path": filepath.ToSlash(relPath)}
	}

	for _, f := range files {
		if !f.IsInit {
			continue
		}
		if nestedUnderInitDir(f.FullPath, sourcePath, initDirs) {
			continue
		}
		handleInitFile(tree, f, projectDir)
	}
}

func insideInitDir(filePath, sourcePath string, initDirs map[string]bool) bool {
	dir := filepath.Dir(filePath)
	for {
		if initDirs[dir] {
			return true
		}
		if dir == sourcePath || !strings.HasPrefix(dir, sourcePath) {
			return false
		}
		dir = filepath.Dir(dir)
	}
}

func nestedUnderInitDir(initPath, sourcePath string, initDirs map[string]bool) bool {
	dir := filepath.Dir(filepath.Dir(initPath))
	for {
		if initDirs[dir] {
			return true
		}
		if dir == sourcePath || !strings.HasPrefix(dir, sourcePath) {
			return false
		}
		dir = filepath.Dir(dir)
	}
}

func handleInitFile(tree map[string]any, f sourceFile, projectDir string) {
	target := serviceTargets[f.Realm]

	service := folder(tree, target.Service, target.Service)
	parent := service
	if target.Subfolder != "" {
		parent = folder(service, target.Subfolder, "Folder")
	}

	node := folder(parent, f.Feature, "Folder")
	for _, part := range f.SubDirs {
		node = folder(node, part, "Folder")
	}

	dir := filepath.Dir(f.FullPath)
	relDir, err := filepath.Rel(projectDir, dir)
	if err != nil {
		relDir = dir
	}
	node["$path"] = filepath.ToSlash(relDir)
	delete(node, "$className")
}

// returns the child map under key, creating it as className if missing
func folder(parent map[string]any, key, className string) map[string]any {
	node, ok := parent[key].(map[string]any)
	if !ok {
		node = map[string]any{}
		parent[key] = node
	}
	if className != "" {
		node["$className"] = className
	}
	return node
}

func writeIfChanged(projectPath string, project *RojoProjectFile, original []byte) (bool, error) {
	data, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')

	if bytes.Equal(data, original) {
		return false, nil
	}
	if err := os.WriteFile(projectPath, data, 0644); err != nil {
		return false, err
	}
	return true, nil
}
