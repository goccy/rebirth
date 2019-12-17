package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"rebirth"
	"syscall"

	"golang.org/x/xerrors"
)

func run() error {
	cfg, err := rebirth.LoadConfig("rebirth.yml")
	if err != nil {
		return xerrors.Errorf("failed to load config: %w", err)
	}

	reloader := rebirth.NewReloader(cfg)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGQUIT)

	go func() {
		for {
			<-sig
			fmt.Println("close...")
			if err := reloader.Close(); err != nil {
				log.Printf("%+v", err)
			}
			os.Exit(0)
		}
	}()

	if reloader.IsEnabledReload() {
		go rebirth.NewWatcher().Run(func() {
			if err := reloader.Reload(); err != nil {
				fmt.Println(err)
			}
		})
	}
	if err := reloader.Run(); err != nil {
		return xerrors.Errorf("failed to run reloader: %w", err)
	}
	return nil
}

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Printf("%+v", err)
	}
}
