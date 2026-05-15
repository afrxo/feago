// internal/watch.go
package internal

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDelay = 200 * time.Millisecond

func WatchCommand(flags map[string]string, values []string) error {
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

	rebuild := func() {
		if err := Build(wd, sourceDir, rojoProjectFile); err != nil {
			log.Println(Red("build error:"), err)
			return
		}
		log.Println(Green("rebuilt"))
	}

	if err := Build(wd, sourceDir, rojoProjectFile); err != nil {
		log.Println(Red("initial build error:"), err)
	} else {
		log.Println(Green("initial build ok"))
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

	log.Println(Dim("watching"), sourcePath)

	var (
		mu    sync.Mutex
		timer *time.Timer
	)
	schedule := func() {
		mu.Lock()
		defer mu.Unlock()
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
			log.Println("shutting down")
			return nil
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Println("watch error:", err)
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
						log.Println("watch add error:", err)
					}
					schedule()
					continue
				}
			}

			// todo: whos still on .lua files -.-
			if strings.HasSuffix(ev.Name, ".luau") || strings.HasSuffix(ev.Name, ".feago") {
				schedule()
			}
		}
	}
}
