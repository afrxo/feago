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

// `--@load:<realm>` in Luau, `//@load:<realm>` in TypeScript
var directiveRe = regexp.MustCompile(`^(--|//)@load:(\S*)\s*$`)

const directiveScanLimit = 4096

var validRealms = map[string]string{
	"server":  "Server",
	"client":  "Client",
	"preload": "Preload",
	"shared":  "Shared",
}

type RojoProjectFile struct {
	Name              string
	Tree              map[string]any
	EmitLegacyScripts *bool

	// top-level fields feago doesn't manage (globIgnorePaths, servePort, ...),
	// kept as-is so a rebuild doesn't drop them
	Extra map[string]json.RawMessage
}

func (p *RojoProjectFile) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for key, raw := range fields {
		var err error
		switch key {
		case "name":
			err = json.Unmarshal(raw, &p.Name)
		case "tree":
			err = json.Unmarshal(raw, &p.Tree)
		case "emitLegacyScripts":
			err = json.Unmarshal(raw, &p.EmitLegacyScripts)
		default:
			if p.Extra == nil {
				p.Extra = map[string]json.RawMessage{}
			}
			p.Extra[key] = raw
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// writes name, extra fields (sorted), tree, emitLegacyScripts
func (p RojoProjectFile) MarshalJSON() ([]byte, error) {
	type field struct {
		key   string
		value any
	}
	fields := []field{{"name", p.Name}}
	extraKeys := make([]string, 0, len(p.Extra))
	for k := range p.Extra {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		fields = append(fields, field{k, p.Extra[k]})
	}
	fields = append(fields, field{"tree", p.Tree})
	if p.EmitLegacyScripts != nil {
		fields = append(fields, field{"emitLegacyScripts", *p.EmitLegacyScripts})
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, _ := json.Marshal(f.key)
		value, err := json.Marshal(f.value)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
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
	RojoPath string // file Rojo serves: FullPath, or the compiled output for roblox-ts
	IsInit   bool
}

type BuildResult struct {
	Files    int
	Changed  bool
	Features []string
	Warnings int
}

var buildWarnings int

func BuildCommand(flags map[string]string, values []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	rojoProjectFile := "default.project.json"
	if project, ok := flags["project"]; ok {
		rojoProjectFile = project
	}

	sourceDir := ""
	if len(values) > 0 {
		sourceDir = values[0]
	}

	_, err = Build(wd, sourceDir, rojoProjectFile, false)
	return err
}

// an empty sourceDir means the default: tsconfig rootDir for roblox-ts, src/ otherwise.
func Build(wd, sourceDir, rojoProjectFile string, quiet bool) (*BuildResult, error) {
	buildWarnings = 0
	projectPath := filepath.Join(wd, rojoProjectFile)
	projectDir := filepath.Dir(projectPath)

	rbxts, err := loadRbxtsProject(projectDir)
	if err != nil {
		return nil, err
	}
	sourceDir = defaultSourceDir(sourceDir, rbxts, wd)

	if !quiet {
		fmt.Fprintf(os.Stdout, "%s %s %s\n",
			BoldYellow("feago"), Yellow(Version),
			Dim(fmt.Sprintf("%s Building %s %s %s%s", SymDot, sourceDir, SymInfo, rojoProjectFile, rbxtsTag(rbxts))),
		)
	}
	sourcePath := resolveSourceDir(sourceDir, wd, projectDir)

	project, raw, err := loadProject(projectPath, rojoProjectFile, wd)
	if err != nil {
		return nil, err
	}

	if rbxts != nil && !isWithin(sourcePath, rbxts.RootDir) {
		return nil, fmt.Errorf("Source directory is outside tsconfig rootDir: %s\n  %s", sourceDir, Dim(SymDot+" rbxtsc only compiles files under rootDir"))
	}

	files, err := collectSourceFiles(sourcePath, sourceDir, projectDir, rbxts)
	if err != nil {
		return nil, err
	}

	emitLegacy := false
	project.EmitLegacyScripts = &emitLegacy

	if project.Tree == nil {
		project.Tree = map[string]any{}
	}
	resetManagedSubtrees(project.Tree)
	populateTree(project.Tree, files, projectDir, sourcePath, rbxts != nil)

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

	res := &BuildResult{Files: len(files), Changed: changed, Features: features, Warnings: buildWarnings}

	if quiet {
		return res, nil
	}

	featuresLabel := "feature"
	if len(features) > 1 {
		featuresLabel = "features"
	}

	count := Dim(fmt.Sprintf("%s %d files %s %d %s", SymDot, len(files), SymDot, len(features), featuresLabel))
	verb := Green(SymOK + " Built")
	if !changed {
		verb = Dim(SymDot + " Unchanged")
	}
	if buildWarnings > 0 {
		fmt.Fprintln(os.Stdout)
	}
	fmt.Fprintf(os.Stdout, "%s %s\n", verb, count)
	if len(features) > 0 {
		fmt.Fprintf(os.Stdout, "  %s  %s\n", Dim("Features"), formatFeatures(features))
	}
	return res, nil
}

func defaultSourceDir(sourceDir string, rbxts *rbxtsProject, wd string) string {
	if sourceDir != "" {
		return sourceDir
	}
	if rbxts == nil {
		return "src"
	}
	if rel, err := filepath.Rel(wd, rbxts.RootDir); err == nil {
		return rel
	}
	return rbxts.RootDir
}

func rbxtsTag(rbxts *rbxtsProject) string {
	if rbxts == nil {
		return ""
	}
	return " " + SymDot + " roblox-ts"
}

func formatFeatures(features []string) string {
	parts := make([]string, len(features))
	for i, f := range features {
		parts[i] = Bold(f)
	}
	return strings.Join(parts, Dim(", "))
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
			return nil, nil, fmt.Errorf("Project file not found: %s\n  %s", name, Dim(SymDot+" Create one, or pass --project <file>"))
		}
		return nil, nil, err
	}

	project := &RojoProjectFile{}
	if err := json.Unmarshal(raw, project); err != nil {
		return nil, nil, err
	}
	return project, raw, nil
}

func collectSourceFiles(sourcePath, sourceDir, projectDir string, rbxts *rbxtsProject) ([]sourceFile, error) {
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Source directory not found: %s\n  %s", sourceDir, Dim(SymDot+" Create it, or pass `feago build <dir>`"))
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
			comment := sourceComment(base, rbxts != nil)
			if comment == "" {
				return nil
			}

			rel, _ := filepath.Rel(featurePath, path)
			subDirs := splitSubDirs(filepath.Dir(rel))

			folderRealm := resolveFolderRealm(filepath.Dir(path), featurePath, sidecarCache)

			realm, err := classify(path, base, comment, folderRealm)
			if err != nil {
				return err
			}

			rojoPath := path
			if rbxts != nil {
				rojoPath = rbxts.outputPath(path)
			}

			name := stripScriptSuffix(filepath.Base(rojoPath))
			files = append(files, sourceFile{
				Realm:    realm,
				Feature:  feature,
				SubDirs:  subDirs,
				Name:     name,
				FullPath: path,
				RojoPath: rojoPath,
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

// returns the line comment prefix for directives in a source file feago
// maps, or "" for anything else. .ts/.tsx only count in roblox-ts projects.
func sourceComment(base string, rbxts bool) string {
	switch filepath.Ext(base) {
	case ".luau":
		return "--"
	case ".ts", ".tsx":
		if rbxts && isCompiledTS(base) {
			return "//"
		}
	}
	return ""
}

// "server" or "client" for Rojo's script suffixes, "" for modules
func scriptSuffix(base string) string {
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	switch {
	case strings.HasSuffix(stem, ".server"):
		return "server"
	case strings.HasSuffix(stem, ".client"):
		return "client"
	}
	return ""
}

func classify(path, filename, comment, folderRealm string) (string, error) {
	directive, err := scanDirective(path, comment)
	if err != nil {
		return "", err
	}

	switch scriptSuffix(filename) {
	case "server":
		if directive != "" && directive != "server" {
			warnDirective(path, comment, directive, "Filename suffix `.server` takes precedence")
		}
		return "Server", nil
	case "client":
		if directive == "preload" {
			return "Preload", nil
		}
		if directive != "" && directive != "client" && directive != "preload" {
			warnDirective(path, comment, directive, "Filename suffix `.client` takes precedence")
		}
		return "Client", nil
	}

	if directive != "" {
		if realm, ok := validRealms[directive]; ok {
			return realm, nil
		}
		warnDirective(path, comment, directive, "Expected one of: server, client, preload, shared")
	}
	if folderRealm != "" {
		return folderRealm, nil
	}
	return "Shared", nil
}

func warnDirective(path, comment, value, hint string) {
	buildWarnings++
	label := value
	if label == "" {
		label = "<empty>"
	}
	display := path
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil {
			display = rel
		}
	}
	fmt.Fprintf(os.Stderr, "%s Unknown directive `%s@load:%s` in %s\n  %s\n",
		Yellow(SymWarn+" Warn"), comment, label, display, Dim(SymDot+" "+hint))
}

// reads up to directiveScanLimit bytes from path and looks for
// `<comment>@load:<realm>` in the leading comment lines. Returns "" if absent or invalid.
func scanDirective(path, comment string) (string, error) {
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
			if m := directiveRe.FindStringSubmatch(trimmed); m != nil && m[1] == comment {
				return m[2], nil
			}
			// stop at first non-blank line if not recognized
			if !strings.HasPrefix(trimmed, comment) {
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
		buildWarnings++
		fmt.Fprintf(os.Stderr, "%s Unknown realm %q in %s\n", Yellow(SymWarn+" Warn"), v, filepath.Join(dir, ".feago"))
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

func populateTree(tree map[string]any, files []sourceFile, projectDir, sourcePath string, optional bool) {
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

		relPath, err := filepath.Rel(projectDir, f.RojoPath)
		if err != nil {
			relPath = f.RojoPath
		}
		current[f.Name] = map[string]any{"$path": rojoPathValue(relPath, optional)}
	}

	for _, f := range files {
		if !f.IsInit {
			continue
		}
		if nestedUnderInitDir(f.FullPath, sourcePath, initDirs) {
			continue
		}
		handleInitFile(tree, f, projectDir, optional)
	}
}

// compiled roblox-ts output may not exist yet (before the first rbxtsc run,
// or for a file that failed to compile). Optional paths keep Rojo serving
// the rest of the project instead of rejecting it.
func rojoPathValue(rel string, optional bool) any {
	rel = filepath.ToSlash(rel)
	if optional {
		return map[string]any{"optional": rel}
	}
	return rel
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

func handleInitFile(tree map[string]any, f sourceFile, projectDir string, optional bool) {
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

	dir := filepath.Dir(f.RojoPath)
	relDir, err := filepath.Rel(projectDir, dir)
	if err != nil {
		relDir = dir
	}
	node["$path"] = rojoPathValue(relDir, optional)
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
