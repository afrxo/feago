package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const tsconfigFixture = `{
	"compilerOptions": {
		// required
		"strict": true,
		"typeRoots": ["node_modules/@rbxts"],

		// configurable
		"rootDir": "src",
		"outDir": "out", /* trailing comma */
	},
}
`

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func buildTree(t *testing.T, root string) map[string]any {
	t.Helper()
	if _, err := Build(root, "", "default.project.json", true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "default.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var project RojoProjectFile
	if err := json.Unmarshal(raw, &project); err != nil {
		t.Fatal(err)
	}
	return project.Tree
}

// returns the $path at tree/a/b/c, or nil if the node is missing
func pathAt(tree map[string]any, keys ...string) any {
	node := tree
	for _, k := range keys {
		child, ok := node[k].(map[string]any)
		if !ok {
			return nil
		}
		node = child
	}
	return node["$path"]
}

func optional(p string) any {
	return map[string]any{"optional": p}
}

func TestBuildLuau(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"default.project.json":   `{"name": "Game", "tree": {"$className": "DataModel"}}`,
		"src/A/Mod.luau":         "return {}",
		"src/A/Run.server.luau":  "print(1)",
		"src/A/Ui.client.luau":   "print(1)",
		"src/A/Boot.client.luau": "-- boot\n--@load:preload\nprint(1)",
		"src/A/Mod.ts":           "export {}", // ignored without tsconfig.json
	})

	tree := buildTree(t, root)
	cases := []struct {
		keys []string
		want any
	}{
		{[]string{"ReplicatedStorage", "Shared", "A", "Mod"}, "src/A/Mod.luau"},
		{[]string{"ServerScriptService", "A", "Run"}, "src/A/Run.server.luau"},
		{[]string{"ReplicatedStorage", "Client", "A", "Ui"}, "src/A/Ui.client.luau"},
		{[]string{"ReplicatedFirst", "A", "Boot"}, "src/A/Boot.client.luau"},
	}
	for _, c := range cases {
		if got := pathAt(tree, c.keys...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", strings.Join(c.keys, "/"), got, c.want)
		}
	}
}

func TestBuildRbxts(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"tsconfig.json":                    tsconfigFixture,
		"default.project.json":             `{"name": "Game", "tree": {"$className": "DataModel"}}`,
		"src/Combat/Damage.ts":             "// math\n//@load:server\nexport {}",
		"src/Combat/Combat.server.ts":      "",
		"src/Combat/Hud.client.tsx":        "",
		"src/Combat/Boot.client.ts":        "//@load:preload\n",
		"src/Combat/Legacy.luau":           "--@load:client\nreturn {}",
		"src/Combat/types.d.ts":            "",
		"src/Combat/Wrong.ts":              "--@load:server\n", // Luau directive syntax doesn't count in TS
		"src/Inventory/Slots/index.ts":     "",
		"src/Inventory/Slots/Util.ts":      "",
		"src/Admin/.feago":                 "realm = server\n",
		"src/Admin/Kick.ts":                "",
		"src/Admin/Panel/index.client.ts":  "",
		"src/Admin/Panel/Button.client.ts": "",
	})

	tree := buildTree(t, root)
	cases := []struct {
		keys []string
		want any
	}{
		{[]string{"ServerScriptService", "Combat", "Damage"}, optional("out/Combat/Damage.luau")},
		{[]string{"ServerScriptService", "Combat", "Combat"}, optional("out/Combat/Combat.server.luau")},
		{[]string{"ReplicatedStorage", "Client", "Combat", "Hud"}, optional("out/Combat/Hud.client.luau")},
		{[]string{"ReplicatedFirst", "Combat", "Boot"}, optional("out/Combat/Boot.client.luau")},
		{[]string{"ReplicatedStorage", "Client", "Combat", "Legacy"}, optional("out/Combat/Legacy.luau")},
		{[]string{"ReplicatedStorage", "Shared", "Combat", "Wrong"}, optional("out/Combat/Wrong.luau")},
		{[]string{"ReplicatedStorage", "Shared", "Combat", "types"}, nil},
		{[]string{"ReplicatedStorage", "Shared", "Inventory", "Slots"}, optional("out/Inventory/Slots")},
		{[]string{"ReplicatedStorage", "Shared", "Inventory", "Slots", "Util"}, nil},
		{[]string{"ServerScriptService", "Admin", "Kick"}, optional("out/Admin/Kick.luau")},
		{[]string{"ReplicatedStorage", "Client", "Admin", "Panel"}, optional("out/Admin/Panel")},
	}
	for _, c := range cases {
		if got := pathAt(tree, c.keys...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", strings.Join(c.keys, "/"), got, c.want)
		}
	}
}

func TestBuildRbxtsSourceOutsideRootDir(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"tsconfig.json":        tsconfigFixture,
		"default.project.json": `{"name": "Game", "tree": {}}`,
		"lib/A/Mod.ts":         "",
	})
	_, err := Build(root, filepath.Join(root, "lib"), "default.project.json", true)
	if err == nil || !strings.Contains(err.Error(), "outside tsconfig rootDir") {
		t.Fatalf("got %v, want rootDir error", err)
	}
}

func TestBuildKeepsUnmanagedFields(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"default.project.json": `{
  "name": "Game",
  "servePort": 34873,
  "globIgnorePaths": ["**/package.json"],
  "tree": {
    "$className": "DataModel",
    "ReplicatedStorage": {
      "rbxts_include": {"$path": "include"}
    }
  }
}`,
		"src/A/Mod.luau": "return {}",
	})

	if _, err := Build(root, "", "default.project.json", true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "default.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)

	for _, want := range []string{`"servePort": 34873`, `"**/package.json"`, `"$path": "include"`} {
		if !strings.Contains(got, want) {
			t.Errorf("project file lost %s:\n%s", want, got)
		}
	}
	order := []string{`"name"`, `"globIgnorePaths"`, `"servePort"`, `"tree"`, `"emitLegacyScripts"`}
	last := -1
	for _, key := range order {
		i := strings.Index(got, key)
		if i < last {
			t.Errorf("%s out of order:\n%s", key, got)
		}
		last = i
	}
}

func TestOutputPath(t *testing.T) {
	p := &rbxtsProject{RootDir: filepath.FromSlash("/p/src"), OutDir: filepath.FromSlash("/p/out"), LuauExt: ".luau"}
	cases := map[string]string{
		"/p/src/A/Mod.ts":           "/p/out/A/Mod.luau",
		"/p/src/A/Mod.tsx":          "/p/out/A/Mod.luau",
		"/p/src/A/Run.server.ts":    "/p/out/A/Run.server.luau",
		"/p/src/A/index.ts":         "/p/out/A/init.luau",
		"/p/src/A/index.client.tsx": "/p/out/A/init.client.luau",
		"/p/src/A/Old.luau":         "/p/out/A/Old.luau",
	}
	for in, want := range cases {
		if got := p.outputPath(filepath.FromSlash(in)); got != filepath.FromSlash(want) {
			t.Errorf("outputPath(%s) = %s, want %s", in, got, want)
		}
	}

	p.LuauExt = ".lua"
	if got := p.outputPath(filepath.FromSlash("/p/src/A/index.ts")); got != filepath.FromSlash("/p/out/A/init.lua") {
		t.Errorf("with luau disabled got %s", got)
	}
}

func TestLoadRbxtsProject(t *testing.T) {
	root := t.TempDir()
	if p, err := loadRbxtsProject(root); p != nil || err != nil {
		t.Fatalf("no tsconfig.json: got %v, %v", p, err)
	}

	writeFiles(t, root, map[string]string{
		"tsconfig.json": `{"compilerOptions": {"rootDir": "game", "outDir": "build/out"}, "rbxts": {"luau": false}}`,
	})
	p, err := loadRbxtsProject(root)
	if err != nil {
		t.Fatal(err)
	}
	want := &rbxtsProject{RootDir: filepath.Join(root, "game"), OutDir: filepath.Join(root, "build", "out"), LuauExt: ".lua"}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("got %+v, want %+v", p, want)
	}
}

func TestStripJSONC(t *testing.T) {
	in := `{
	// line comment
	"url": "https://example.com/*not a comment*/", /* block */
	"escaped": "quote \" // still a string",
	"list": [1, 2,],
}`
	var got map[string]any
	if err := json.Unmarshal(stripJSONC([]byte(in)), &got); err != nil {
		t.Fatalf("%v\n%s", err, stripJSONC([]byte(in)))
	}
	want := map[string]any{
		"url":     "https://example.com/*not a comment*/",
		"escaped": `quote " // still a string`,
		"list":    []any{1.0, 2.0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
