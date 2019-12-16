package rebirth

import (
	"log"

	"golang.org/x/xerrors"
	"gopkg.in/fsnotify.v1"
)

type Watcher struct {
	goWatcher *fsnotify.Watcher
	callback  func()
}

func NewWatcher() *Watcher {
	return &Watcher{}
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
				if event.Op&fsnotify.Write == fsnotify.Write {
					if err := w.notify(); err != nil {
						log.Printf("%+v", err)
					}
				}
				if event.Op&fsnotify.Create == fsnotify.Create {
					if err := w.notify(); err != nil {
						log.Printf("%+v", err)
					}
				}
			case err := <-watcher.Errors:
				log.Printf("%+v", err)
			}
		}
	}()
	w.goWatcher = watcher
	return nil
}

func (w *Watcher) recoverRuntimeError() {
	if err := recover(); err != nil {
		log.Printf("%+v", err)
	}
}

func (w *Watcher) notify() error {
	w.callback()
	return nil
}
