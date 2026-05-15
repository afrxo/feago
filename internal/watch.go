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
		fmt.Fprintf(os.Stdout, "%s %s %s\n", BoldYellow("feago"), Yellow(Version), Dim(SymDot+" watch"))
		fmt.Fprintf(os.Stdout, "%s %s %s\n\n", Blue(SymInfo), sourcePath, Dim(SymDot+" Ctrl+C to stop"))
	}

	rebuild := func() {
		mu.Lock()
		paths := make([]string, 0, len(changed))
		for p := range changed {
			paths = append(paths, p)
		}
		changed = map[string]struct{}{}
		mu.Unlock()

		header()
		for _, p := range paths {
			if rel, err := filepath.Rel(wd, p); err == nil {
				p = rel
			}
			fmt.Fprintf(os.Stdout, "%s %s\n", Blue(SymCycle), p)
		}
		if len(paths) > 0 {
			fmt.Fprintln(os.Stdout)
		}
		if _, err := Build(wd, sourceDir, rojoProjectFile); err != nil {
			fmt.Fprintf(os.Stderr, "%s %v\n", BoldRed(SymErr+" build"), err)
		}
	}

	header()
	if _, err := Build(wd, sourceDir, rojoProjectFile); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", BoldRed(SymErr+" build"), err)
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
			fmt.Fprintf(os.Stdout, "%s\n", Dim("stopped watching"))
			return nil
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "%s watch %v\n", BoldRed(SymErr), err)
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
