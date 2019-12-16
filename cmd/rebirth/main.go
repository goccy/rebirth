package main

import (
	"flag"
	"fmt"
	"rebirth"
)

var (
	isServerMode bool
)

func init() {
	flag.BoolVar(&isServerMode, "server", false, "start as server mode")
}

func main() {
	flag.Parse()

	cfg, err := rebirth.LoadConfig("rebirth.yml")
	if err != nil {
		panic(err)
	}
	reloader := rebirth.NewReloader(cfg)
	if !isServerMode {
		go rebirth.NewWatcher().Run(func() {
			if err := reloader.Reload(); err != nil {
				fmt.Println(err)
			}
		})
	}
	if err := reloader.Run(isServerMode); err != nil {
		panic(err)
	}
}
