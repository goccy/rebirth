package rebirth

import (
	"bytes"
	"context"
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

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/xerrors"
)

const configDir = ".rebirth"

var (
	buildPath         = filepath.Join(configDir, "program")
	pidPath           = filepath.Join(configDir, "server.pid")
	dockerRebirthPath = filepath.Join(configDir, "__rebirth")
)

type Reloader struct {
	host *Host
	cmd  *Command
}

func NewReloader(cfg *Config) *Reloader {
	return &Reloader{
		host: cfg.Host,
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
		go r.execRebirthOnDockerContainer(
			context.Background(),
			r.host.Docker,
		)
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
	command := []string{"kill", "-QUIT", fmt.Sprint(pid)}
	fmt.Println("stop hot reloader on container...")
	if _, err := r.execCommandOnDockerContainer(context.Background(), containerName, command); err != nil {
		return xerrors.Errorf("failed to exec command on docker container: %w", err)
	}
	return nil
}

func (r *Reloader) rebirthDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func (r *Reloader) xbuildRebirth() error {
	cmdFile := filepath.Join(r.rebirthDir(), "cmd", "rebirth", "main.go")
	if err := r.xbuild(dockerRebirthPath, cmdFile); err != nil {
		return xerrors.Errorf("failed to xbuild: %w", err)
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
	fmt.Println("restarting...")
	if err := r.stopCurrentProcess(); err != nil {
		return xerrors.Errorf("failed to stop current process: %w", err)
	}
	execCmd := NewCommand(buildPath)
	r.cmd = execCmd
	go func() {
		if err := execCmd.Run(); err != nil {
			fmt.Println(err)
			execCmd.Stop()
			r.cmd = nil
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

func (r *Reloader) xbuild(target, source string) error {
	env, err := r.buildEnv()
	if err != nil {
		return xerrors.Errorf("failed to get build env: %w", err)
	}
	cmd := NewCommand(
		"go",
		"build",
		"-o",
		target,
		"--ldflags",
		`-linkmode external -extldflags "-static"`,
		source,
	)
	cmd.AddEnv(env)
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to command: %w", err)
	}
	return nil
}

func (r *Reloader) buildEnv() ([]string, error) {
	goos, err := r.buildGOOS()
	if err != nil {
		return nil, xerrors.Errorf("failed to get GOOS for build: %w", err)
	}
	goarch, err := r.buildGOARCH()
	if err != nil {
		return nil, xerrors.Errorf("failed to get GOARCH for build: %w", err)
	}
	return []string{
		fmt.Sprintf("PATH=%s:%s", os.Getenv("PATH"), filepath.Join(r.rebirthDir(), "bin")),
		"CGO_ENABLED=1",
		fmt.Sprintf("CC=%s", r.buildCC()),
		fmt.Sprintf("GOOS=%s", goos),
		fmt.Sprintf("GOARCH=%s", goarch),
	}, nil
}

func (r *Reloader) buildGOOS() (string, error) {
	if r.isUsedDocker() {
		goos, err := r.execCommandOnDockerContainer(
			context.Background(),
			r.host.Docker,
			[]string{"go", "env", "GOOS"},
		)
		if err != nil {
			return "", xerrors.Errorf("failed to get GOOS env on container: %w", err)
		}
		return goos, nil
	}
	return runtime.GOOS, nil
}

func (r *Reloader) buildGOARCH() (string, error) {
	if r.isUsedDocker() {
		goarch, err := r.execCommandOnDockerContainer(
			context.Background(),
			r.host.Docker,
			[]string{"go", "env", "GOARCH"},
		)
		if err != nil {
			return "", xerrors.Errorf("failed to get GOARCH env on container: %w", err)
		}
		return goarch, nil
	}
	return runtime.GOARCH, nil
}

func (r *Reloader) buildCC() string {
	if r.isUsedDocker() {
		return "x86_64-linux-musl-cc"
	}
	return "gcc"
}

func (r *Reloader) execRebirthOnDockerContainer(ctx context.Context, containerName string) error {
	cli, err := client.NewEnvClient()
	if err != nil {
		return xerrors.Errorf("failed to create docker client: %w", err)
	}
	cfg := types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{dockerRebirthPath},
	}
	execResp, err := cli.ContainerExecCreate(ctx, containerName, cfg)
	if err != nil {
		return xerrors.Errorf("failed to ContainerExecCreate: %w", err)
	}
	execID := execResp.ID
	attachResp, err := cli.ContainerExecAttach(ctx, execID, cfg)
	if err != nil {
		return xerrors.Errorf("failed to ContainerExecAttach: %w", err)
	}
	defer attachResp.Close()
	if _, err = stdcopy.StdCopy(os.Stdout, os.Stderr, attachResp.Reader); err != nil {
		return xerrors.Errorf("failed to copy stdout/stderr: %w", err)
	}
	return nil
}

func (r *Reloader) execCommandOnDockerContainer(ctx context.Context, containerName string, command []string) (string, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		return "", xerrors.Errorf("failed to create docker client: %w", err)
	}
	cfg := types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          command,
	}
	execResp, err := cli.ContainerExecCreate(ctx, containerName, cfg)
	if err != nil {
		return "", xerrors.Errorf("failed to ContainerExecCreate: %w", err)
	}
	execID := execResp.ID
	attachResp, err := cli.ContainerExecAttach(ctx, execID, cfg)
	if err != nil {
		return "", xerrors.Errorf("failed to ContainerExecAttach: %w", err)
	}
	defer attachResp.Close()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	if _, err = stdcopy.StdCopy(stdout, stderr, attachResp.Reader); err != nil {
		return "", xerrors.Errorf("failed to copy stdout/stderr: %w", err)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func (r *Reloader) sendReloadingSignal() error {
	if r.host != nil && r.host.Docker != "" {
		pid, err := r.readPID()
		if err != nil {
			return xerrors.Errorf("failed to read pid: %w", err)
		}
		containerName := r.host.Docker
		command := []string{"kill", "-HUP", fmt.Sprint(pid)}
		if _, err := r.execCommandOnDockerContainer(context.Background(), containerName, command); err != nil {
			return xerrors.Errorf("failed to exec command on docker container: %w", err)
		}
		return nil
	}
	return nil
}
