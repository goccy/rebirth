package rebirth

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/xerrors"
	"gopkg.in/fsnotify.v1"
)

type state int

const (
	idleState state = iota
	busyState
)

type Watcher struct {
	goWatcher  *fsnotify.Watcher
	eventCh    chan struct{}
	callback   func()
	watchState state
	mu         sync.Mutex
	cfg        *Watch
}

const (
	defaultRoot = "."
)

func NewWatcher(cfg *Config) *Watcher {
	return &Watcher{
		eventCh:    make(chan struct{}, 1),
		watchState: idleState,
		cfg:        cfg.Watch,
	}
}

func (w *Watcher) addEvent(event fsnotify.Event) {
	name := filepath.Base(event.Name)
	if strings.HasPrefix(name, "#") {
		return
	}
	if strings.HasPrefix(name, ".") {
		return
	}
	if filepath.Ext(name) != ".go" {
		return
	}
	if strings.HasSuffix(name, "_test.go") {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.watchState = busyState
	w.eventCh <- struct{}{}
}

func (w *Watcher) root() string {
	if w.cfg == nil {
		return defaultRoot
	}
	root := w.cfg.Root
	if root == "" {
		return defaultRoot
	}
	return root
}

func (w *Watcher) ignorePaths() []string {
	root := w.root()
	paths := []string{}
	if w.cfg == nil {
		return paths
	}
	for _, path := range w.cfg.Ignore {
		if defaultRoot != "." {
			paths = append(paths, filepath.Join(root, path))
		} else {
			paths = append(paths, path)
		}
	}
	return paths
}

func (w *Watcher) watchPaths() []string {
	ignorePaths := w.ignorePaths()
	pathMap := map[string]struct{}{}
	filepath.Walk(w.root(), func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			return nil
		}
		if strings.HasPrefix(path, ".") {
			return nil
		}
		for _, p := range ignorePaths {
			if strings.HasPrefix(path, p) {
				return nil
			}
		}
		pathMap[path] = struct{}{}
		return nil
	})
	paths := []string{w.root()}
	for path := range pathMap {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (w *Watcher) fileNumForWatching(paths []string) int {
	fileNum := 0
	for _, path := range paths {
		matches, _ := filepath.Glob(filepath.Join(path, "*"))
		fileNum += len(matches)
	}
	return fileNum
}

func (w *Watcher) Run(callback func()) error {
	w.callback = callback
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return xerrors.Errorf("failed to create fsnotify instance: %w", err)
	}
	watchPaths := w.watchPaths()
	fileNum := w.fileNumForWatching(watchPaths)
	for _, path := range watchPaths {
		fmt.Printf("Watching %s\n", path)
		if err := watcher.Add(path); err != nil {
			return xerrors.Errorf(
				"failed to add path %s. current total watching file number is %d: %w",
				path,
				fileNum,
				err,
			)
		}
	}
	go func() {
		defer w.recoverRuntimeError()
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Create == fsnotify.Create {
					w.addEvent(event)
				} else if event.Op&fsnotify.Write == fsnotify.Write {
					w.addEvent(event)
				} else if event.Op&fsnotify.Write == fsnotify.Remove {
					w.addEvent(event)
				} else if event.Op&fsnotify.Write == fsnotify.Rename {
					w.addEvent(event)
				}
			case err := <-watcher.Errors:
				log.Printf("%+v", err)
			}
		}
	}()
	w.goWatcher = watcher

	go func() {
		for {
			switch w.watchState {
			case idleState:
			case busyState:
				func() {
					ctx, cancel := context.WithTimeout(context.Background(), 2000*time.Millisecond)
					defer cancel()
					select {
					case <-w.eventCh:
						// receive event. continue busy phase
					case <-ctx.Done():
						// end busy phase.
						w.mu.Lock()
						defer w.mu.Unlock()
						w.callback()
						if len(w.eventCh) > 0 {
							// exists event. receive it for escaping blocking
							<-w.eventCh
						}
						w.watchState = idleState
					}
				}()
			}
		}
	}()
	return nil
}

func (w *Watcher) recoverRuntimeError() {
	if err := recover(); err != nil {
		log.Printf("%+v", err)
	}
}
