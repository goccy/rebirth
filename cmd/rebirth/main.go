package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/goccy/rebirth"
	"github.com/goccy/rebirth/internal/errors"
	"github.com/jessevdk/go-flags"
	"golang.org/x/xerrors"
)

type Option struct {
	Watch WatchCommand `description:"" command:"watch" hidden:"true"`
	Init  InitCommand  `description:"create rebirth.yml for configuration" command:"init"`
	Run   RunCommand   `description:"execute 'go run'   command"           command:"run"`
	Test  TestCommand  `description:"execute 'go test'  command"           command:"test"`
	Build BuildCommand `description:"execute 'go build' command"           command:"build"`
}

type InitCommand struct{}
type RunCommand struct{}
type TestCommand struct{}
type BuildCommand struct{}
type WatchCommand struct{}

func (cmd *InitCommand) Execute(args []string) error {
	if rebirth.ExistsConfig() {
		return xerrors.New("already exists rebirth.yml")
	}
	if err := ioutil.WriteFile("rebirth.yml", []byte{}, 0644); err != nil {
		return xerrors.Errorf("failed to create rebirth.yml: %w", err)
	}
	return nil
}

func (cmd *RunCommand) Execute(args []string) error {
	if !rebirth.ExistsConfig() {
		return xerrors.New("`rebirth init` must be executed before `rebirth run`")
	}
	cfg, err := rebirth.LoadConfig("rebirth.yml")
	if err != nil {
		return xerrors.Errorf("failed to load config: %w", err)
	}
	gocmd := rebirth.NewGoCommand()
	if cfg.Run != nil {
		env := []string{}
		for k, v := range cfg.Run.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, rebirth.ExpandPath(v)))
		}
		gocmd.AddEnv(env)
	}
	if cfg.Host != nil && cfg.Host.Docker != "" {
		gocmd.EnableCrossBuild(cfg.Host.Docker)
	}
	if err := gocmd.Run(args...); err != nil {
		return xerrors.Errorf("failed to test: %w", err)
	}
	return nil
}

func (cmd *TestCommand) Execute(args []string) error {
	if !rebirth.ExistsConfig() {
		return xerrors.New("`rebirth init` must be executed before `rebirth test`")
	}
	cfg, err := rebirth.LoadConfig("rebirth.yml")
	if err != nil {
		return xerrors.Errorf("failed to load config: %w", err)
	}
	gocmd := rebirth.NewGoCommand()
	if cfg.Build != nil {
		env := []string{}
		for k, v := range cfg.Build.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, rebirth.ExpandPath(v)))
		}
		gocmd.AddEnv(env)
	}
	if cfg.Host != nil && cfg.Host.Docker != "" {
		gocmd.EnableCrossBuild(cfg.Host.Docker)
	}
	if err := gocmd.Test(args...); err != nil {
		return xerrors.Errorf("failed to test: %w", err)
	}
	return nil
}

func (cmd *BuildCommand) Execute(args []string) error {
	if !rebirth.ExistsConfig() {
		return xerrors.New("`rebirth init` must be executed before `rebirth build`")
	}
	cfg, err := rebirth.LoadConfig("rebirth.yml")
	if err != nil {
		return xerrors.Errorf("failed to load config: %w", err)
	}
	gocmd := rebirth.NewGoCommand()
	if cfg.Build != nil {
		env := []string{}
		for k, v := range cfg.Build.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, rebirth.ExpandPath(v)))
		}
		gocmd.AddEnv(env)
	}
	if cfg.Host != nil && cfg.Host.Docker != "" {
		gocmd.EnableCrossBuild(cfg.Host.Docker)
	}
	if err := gocmd.Build(args...); err != nil {
		return xerrors.Errorf("failed to build: %w", err)
	}
	return nil
}

func (cmd *WatchCommand) run() error {
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
		go func() {
			if err := rebirth.NewWatcher(cfg).Run(func() {
				if err := reloader.Reload(); err != nil {
					fmt.Println(err)
				}
			}); err != nil {
				log.Printf("%+v", err)
				os.Exit(1)
			}
		}()
	}
	if err := reloader.Run(); err != nil {
		return xerrors.Errorf("failed to run reloader: %w", err)
	}
	return nil
}

func (cmd *WatchCommand) Execute(args []string) error {
	if err := cmd.run(); err != nil {
		if xerrors.Is(err, errors.ErrCrossCompiler) {
			return errors.ErrCrossCompiler
		}
		log.Printf("%+v", xerrors.Unwrap(err))
	}
	return nil
}

var opts Option

func main() {
	args := []string{os.Args[0]}
	if len(os.Args) == 1 {
		args = append(args, "watch")
	} else {
		args = append(args, os.Args[1])
	}
	args = append(args, "--")
	if len(os.Args) > 2 {
		args = append(args, os.Args[2:]...)
	}
	os.Args = args
	parser := flags.NewParser(&opts, flags.Default)
	parser.Parse()
}
