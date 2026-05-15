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

	sourceDir := "src"
	if len(values) > 0 {
		sourceDir = values[0]
	}

	projectPath := filepath.Join(wd, rojoProjectFile)
	sourcePath := resolveSourceDir(sourceDir, wd, filepath.Dir(projectPath))

	var (
		mu      sync.Mutex
		timer   *time.Timer
		changed = map[string]struct{}{}
	)

	header := func() {
		fmt.Fprint(os.Stdout, "\033[H\033[2J\033[3J")
		fmt.Fprintf(os.Stdout, "%s %s %s\n",
			BoldYellow("feago"), Yellow(Version),
			Dim(fmt.Sprintf("%s Watching %s %s %s %s Ctrl+C to stop", SymDot, sourceDir, SymInfo, rojoProjectFile, SymDot)),
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

	runBuild := func() {
		res, err := Build(wd, sourceDir, rojoProjectFile, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %v\n", BoldRed(SymErr+" Build"), err)
			return
		}
		printStatus(res)
	}

	rebuild := func() {
		mu.Lock()
		changed = map[string]struct{}{}
		mu.Unlock()

		runBuild()
	}

	header()
	runBuild()

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

	err = filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	schedule := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		if name != "" {
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

			if ev.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					if err := watcher.Add(ev.Name); err != nil {
						fmt.Fprintf(os.Stderr, "%s watch add %v\n", BoldRed(SymErr), err)
					}
					schedule(ev.Name)
					continue
				}
			}

			// todo: whos still on .lua files -.-
			if strings.HasSuffix(ev.Name, ".luau") || strings.HasSuffix(ev.Name, ".feago") {
				schedule(ev.Name)
			}
		}
	}
}
