package rebirth

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/xerrors"
)

var (
	cwd               string
	configDir         string
	buildPath         string
	pidPath           string
	dockerRebirthPath string
	binPath           string
	pkgPath           string
)

func init() {
	cwd, _ = os.Getwd()
	configDir = ".rebirth"
	buildPath = filepath.Join(cwd, configDir, "program")
	pidPath = filepath.Join(configDir, "server.pid")
	dockerRebirthPath = filepath.Join(configDir, "__rebirth")
	binPath = filepath.Join(configDir, "bin")
	pkgPath = filepath.Join(configDir, "pkg")
}

type Reloader struct {
	host  *Host
	cmd   *Command
	build *Build
	run   *Run
}

func NewReloader(cfg *Config) *Reloader {
	return &Reloader{
		host:  cfg.Host,
		build: cfg.Build,
		run:   cfg.Run,
	}
}

func (r *Reloader) Run() error {
	if !r.IsEnabledReload() {
		if err := r.writePID(); err != nil {
			return xerrors.Errorf("failed to write pid: %w", err)
		}
		if err := r.reload(); err != nil {
			return xerrors.Errorf("failed to reload: %w", err)
		}
	} else if r.isUsedDocker() && !r.isOnDockerContainer() {
		if err := r.xbuildRebirth(); err != nil {
			return xerrors.Errorf("failed to cross compile for rebirth: %w", err)
		}
		go NewDockerCommand(r.host.Docker, dockerRebirthPath).Run()
	}
	r.watchReloadSignal()
	for {
		time.Sleep(1 * time.Second)
	}
	return nil
}

func (r *Reloader) IsEnabledReload() bool {
	if !r.isUsedDocker() {
		return true
	}
	if !r.isOnDockerContainer() {
		return true
	}
	return false
}

func (r *Reloader) Reload() error {
	fmt.Println("Building....")
	if err := r.xbuild(buildPath, "."); err != nil {
		return xerrors.Errorf("failed to build on host: %w", err)
	}
	if err := r.sendReloadingSignal(); err != nil {
		return xerrors.Errorf("failed to send reloading signal: %w", err)
	}
	return nil
}

func (r *Reloader) Close() error {
	if !r.isUsedDocker() {
		return nil
	}
	if r.isOnDockerContainer() {
		fmt.Println("stop current process...")
		if err := r.stopCurrentProcess(); err != nil {
			return xerrors.Errorf("failed to stop current process: %w", err)
		}
		return nil
	}

	pid, err := r.readPID()
	if err != nil {
		return xerrors.Errorf("failed to read pid: %w", err)
	}
	containerName := r.host.Docker
	fmt.Println("stop hot reloader on container...")
	if err := NewDockerCommand(containerName, "kill", "-QUIT", fmt.Sprint(pid)).Run(); err != nil {
		return xerrors.Errorf("failed to exec command on docker container: %w", err)
	}
	return nil
}

func (r *Reloader) isUsedDocker() bool {
	return r.host != nil && r.host.Docker != ""
}

func (r *Reloader) isOnDockerContainer() bool {
	_, err := os.Stat(filepath.Join("/", ".dockerenv"))
	return err == nil
}

func (r *Reloader) readPID() (int, error) {
	file, err := ioutil.ReadFile(pidPath)
	if err != nil {
		return -1, xerrors.Errorf("failed to read pid file: %w", err)
	}
	pid, err := strconv.ParseInt(string(file), 10, 64)
	if err != nil {
		return -1, xerrors.Errorf("failed to parse pid number: %w", err)
	}
	return int(pid), nil
}

func (r *Reloader) writePID() error {
	pid := os.Getpid()
	if err := ioutil.WriteFile(pidPath, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		return xerrors.Errorf("failed to write pid file: %w", err)
	}
	return nil
}

func (r *Reloader) stopCurrentProcess() error {
	if r.cmd == nil {
		return nil
	}
	if err := r.cmd.Stop(); err != nil {
		return xerrors.Errorf("failed to stop process: %w", err)
	}
	r.cmd = nil
	return nil
}

func (r *Reloader) reload() (e error) {
	fmt.Println("Restarting...")
	if err := r.stopCurrentProcess(); err != nil {
		return xerrors.Errorf("failed to stop current process: %w", err)
	}
	execCmd := NewCommand(buildPath)
	if r.run != nil {
		env := []string{}
		for k, v := range r.run.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		execCmd.AddEnv(env)
	}
	r.cmd = execCmd
	go func() {
		if err := execCmd.Run(); err != nil {
			fmt.Println(err)
		}
	}()
	return nil
}

func (r *Reloader) watchReloadSignal() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)

	go func() {
		for {
			<-sig
			go r.reload()
		}
	}()
}

func (r *Reloader) rebirthDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func (r *Reloader) expandPath(path string) string {
	if strings.HasPrefix(path, "./") {
		absPath, err := filepath.Abs(path)
		if err == nil {
			return absPath
		}
		return path
	}
	if strings.HasPrefix(path, "-I./") {
		absPath, err := filepath.Abs(path[2:])
		if err == nil {
			return fmt.Sprintf("-I%s", absPath)
		}
		return fmt.Sprintf("-I%s", path)
	}
	if strings.HasPrefix(path, "-L./") {
		absPath, err := filepath.Abs(path[2:])
		if err == nil {
			return fmt.Sprintf("-L%s", absPath)
		}
		return fmt.Sprintf("-L%s", path)
	}
	return path
}

func (r *Reloader) xbuildRebirth() error {
	cmdFile := filepath.Join(r.rebirthDir(), "cmd", "rebirth", "main.go")
	gocmd := NewGoCommand()
	gocmd.EnableCrossBuild(r.host.Docker)
	gocmd.SetDir(r.rebirthDir())
	if r.build != nil {
		env := []string{}
		for k, v := range r.build.Env {
			v = r.expandPath(v)
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		gocmd.AddEnv(env)
	}
	if err := gocmd.Build("-o", filepath.Join(cwd, dockerRebirthPath), cmdFile); err != nil {
		return xerrors.Errorf("failed to cross build rebirth: %w", err)
	}
	return nil
}

func (r *Reloader) xbuild(target, source string) error {
	gocmd := NewGoCommand()
	if r.build != nil {
		env := []string{}
		for k, v := range r.build.Env {
			v = r.expandPath(v)
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		gocmd.AddEnv(env)
	}
	if r.isUsedDocker() && !r.isOnDockerContainer() {
		gocmd.EnableCrossBuild(r.host.Docker)
	}
	if err := gocmd.Build("-o", target, source); err != nil {
		return xerrors.Errorf("failed to build: %w", err)
	}
	return nil
}

func (r *Reloader) sendReloadingSignal() error {
	if r.host != nil && r.host.Docker != "" {
		pid, err := r.readPID()
		if err != nil {
			return xerrors.Errorf("failed to read pid: %w", err)
		}
		containerName := r.host.Docker
		if err := NewDockerCommand(containerName, "kill", "-HUP", fmt.Sprint(pid)).Run(); err != nil {
			return xerrors.Errorf("failed to exec command on docker container: %w", err)
		}
		return nil
	}
	if err := r.reload(); err != nil {
		return xerrors.Errorf("failed to reload: %w", err)
	}
	return nil
}
