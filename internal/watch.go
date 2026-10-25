// internal/watch.go
package internal

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

func suppressCtrlCEcho() func() {
	if runtime.GOOS == "windows" {
		return func() {}
	}
	cmd := exec.Command("stty", "-echoctl")
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return func() {}
	}
	return func() {
		c := exec.Command("stty", "echoctl")
		c.Stdin = os.Stdin
		_ = c.Run()
	}
}

const debounceDelay = 200 * time.Millisecond

func WatchCommand(flags map[string]string, values []string) error {
	restore := suppressCtrlCEcho()
	defer restore()

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	rojoProjectFile := "default.project.json"
	if project, ok := flags["project"]; ok {
		rojoProjectFile = project
	}

	projectPath := filepath.Join(wd, rojoProjectFile)
	projectDir := filepath.Dir(projectPath)

	rbxts, err := loadRbxtsProject(projectDir)
	if err != nil {
		return err
	}

	sourceDir := ""
	if len(values) > 0 {
		sourceDir = values[0]
	}
	sourceDir = defaultSourceDir(sourceDir, rbxts, wd)
	sourcePath := resolveSourceDir(sourceDir, wd, projectDir)

	var (
		mu      sync.Mutex
		timer   *time.Timer
		changed = map[string]struct{}{}
		resync  bool
	)

	header := func() {
		fmt.Fprint(os.Stdout, "\033[H\033[2J\033[3J")
		fmt.Fprintf(os.Stdout, "%s %s %s\n",
			BoldYellow("feago"), Yellow(Version),
			Dim(fmt.Sprintf("%s Watching %s %s %s%s %s Ctrl+C to stop", SymDot, sourceDir, SymInfo, rojoProjectFile, rbxtsTag(rbxts), SymDot)),
		)
	}

	printStatus := func(res *BuildResult) {
		ts := time.Now().Format("15:04:05")
		featuresLabel := "feature"
		if len(res.Features) > 1 {
			featuresLabel = "features"
		}
		suffix := ""
		if res.Changed {
			suffix = "  " + SymDot + "  project.json updated"
		}
		if res.Warnings > 0 {
			fmt.Fprintln(os.Stdout)
		}
		fmt.Fprintf(os.Stdout,
			"%s [%s] %s\n",
			Green(SymOK+" Rebuilt"),
			ts,
			Dim(fmt.Sprintf("%s %d files %s %d %s%s", SymDot, res.Files, SymDot, len(res.Features), featuresLabel, suffix)),
		)
	}

	runBuild := func(report bool) *BuildResult {
		res, err := Build(wd, sourceDir, rojoProjectFile, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %v\n", BoldRed(SymErr+" Build"), err)
			return nil
		}
		if report || res.Changed {
			printStatus(res)
		}
		return res
	}

	rebuild := func() {
		mu.Lock()
		report := len(changed) > 0
		needResync := resync
		changed = map[string]struct{}{}
		resync = false
		mu.Unlock()

		res := runBuild(report)

		// Rojo only notices a compiled file that appeared after the project
		// file mapped it once the project file is written again
		if needResync && res != nil && !res.Changed {
			if err := rewriteFile(projectPath); err != nil {
				fmt.Fprintf(os.Stderr, "%s %v\n", BoldRed(SymErr+" Build"), err)
			}
		}
	}

	header()
	runBuild(true)

	if _, err := os.Stat(sourcePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Source dir not found: %s\n  %s", sourceDir, Dim(SymDot+" Create it, or pass a different path"))
		}
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	watchTree := func(root string) error {
		return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return watcher.Add(path)
			}
			return nil
		})
	}

	if err := watchTree(sourcePath); err != nil {
		return err
	}

	// rbxtsc compiles into outDir, which may not exist before its first run
	outPath := ""
	if rbxts != nil && !isWithin(rbxts.OutDir, sourcePath) && !isWithin(sourcePath, rbxts.OutDir) {
		outPath = rbxts.OutDir
		if err := os.MkdirAll(outPath, 0755); err != nil {
			return err
		}
		if err := watchTree(outPath); err != nil {
			return err
		}
	}

	// output: a compiled file showed up in outDir, so Rojo needs a resync
	schedule := func(name string, output bool) {
		mu.Lock()
		defer mu.Unlock()
		if output {
			resync = true
		} else if name != "" {
			changed[name] = struct{}{}
		}
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounceDelay, rebuild)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-stop:
			fmt.Fprintf(os.Stdout, "%s %s\n", Bold("■ Stopped"), Dim("watching"))
			return nil
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "%s Watch %v\n", BoldRed(SymErr), err)
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if ev.Name == projectPath {
				continue
			}

			if outPath != "" && isWithin(ev.Name, outPath) {
				if ev.Op&fsnotify.Create != fsnotify.Create {
					continue
				}
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					// rbxtsc creates nested dirs and their files in one go
					if err := watchTree(ev.Name); err != nil {
						fmt.Fprintf(os.Stderr, "%s watch add %v\n", BoldRed(SymErr), err)
					}
					schedule(ev.Name, true)
				} else if ext := filepath.Ext(ev.Name); ext == ".luau" || ext == ".lua" {
					schedule(ev.Name, true)
				}
				continue
			}

			if ev.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					if err := watcher.Add(ev.Name); err != nil {
						fmt.Fprintf(os.Stderr, "%s watch add %v\n", BoldRed(SymErr), err)
					}
					schedule(ev.Name, false)
					continue
				}
			}

			// todo: whos still on .lua files -.-
			if sourceComment(filepath.Base(ev.Name), rbxts != nil) != "" || strings.HasSuffix(ev.Name, ".feago") {
				schedule(ev.Name, false)
			}
		}
	}
}

// writes path back unchanged, so file watchers (Rojo) re-read it
func rewriteFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
