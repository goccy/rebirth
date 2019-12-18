package rebirth

import (
	"context"
	"log"
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
}

func NewWatcher() *Watcher {
	return &Watcher{
		eventCh:    make(chan struct{}, 1),
		watchState: idleState,
	}
}

func (w *Watcher) addEvent(event fsnotify.Event) {
	if strings.HasPrefix(event.Name, "#") {
		return
	}
	if strings.HasPrefix(event.Name, ".") {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.watchState = busyState
	w.eventCh <- struct{}{}
}

func (w *Watcher) Run(callback func()) error {
	w.callback = callback
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return xerrors.Errorf("failed to create fsnotify instance: %w", err)
	}
	path := "."
	if err := watcher.Add(path); err != nil {
		return xerrors.Errorf("failed to add path %s: %w", path, err)
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
