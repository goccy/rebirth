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
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/goccy/go-yaml"
	"golang.org/x/xerrors"
)

const configDir = ".rebirth"

var (
	buildContextPath = filepath.Join(configDir, "build.yml")
	buildPath        = filepath.Join(configDir, "program")
	pidPath          = filepath.Join(configDir, "server.pid")
)

type buildContext struct {
	GOOS   string `yaml:"goos"`
	GOARCH string `yaml:"goarch"`
}

func (c *buildContext) toEnv() []string {
	return []string{
		fmt.Sprintf("GOOS=%s", c.GOOS),
		fmt.Sprintf("GOARCH=%s", c.GOARCH),
	}
}

type Reloader struct {
	host *Host
	cmd  *Command
}

func NewReloader(cfg *Config) *Reloader {
	return &Reloader{
		host: cfg.Host,
	}
}

func (r *Reloader) Run(isServerMode bool) error {
	if isServerMode {
		if err := r.writeBuildContext(); err != nil {
			return xerrors.Errorf("failed to write build context: %w", err)
		}
		if err := r.writePID(); err != nil {
			return xerrors.Errorf("failed to write pid: %w", err)
		}
	}
	r.watchReloadSignal()
	for {
		time.Sleep(1 * time.Second)
	}
	return nil
}

func (r *Reloader) Reload() error {
	if err := r.buildOnHostOS(); err != nil {
		return xerrors.Errorf("failed to build on host: %w", err)
	}
	if err := r.sendReloadingSignal(); err != nil {
		return xerrors.Errorf("failed to send reloading signal: %w", err)
	}
	return nil
}

func (r *Reloader) loadBuildContext() (*buildContext, error) {
	file, err := ioutil.ReadFile(buildContextPath)
	if err != nil {
		return nil, xerrors.Errorf("failed to read build config file: %w", err)
	}
	var ctx buildContext
	if err := yaml.Unmarshal(file, &ctx); err != nil {
		fmt.Println(yaml.FormatError(err, true, true))
		return nil, nil
	}
	return &ctx, nil
}

func (r *Reloader) writeBuildContext() error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return xerrors.Errorf("failed to create config directory for rebirth: %w", err)
	}
	ctx, err := yaml.Marshal(&buildContext{
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
	})
	if err != nil {
		return xerrors.Errorf("failed to encode build context: %w", err)
	}
	if err := ioutil.WriteFile(buildContextPath, ctx, 0644); err != nil {
		return xerrors.Errorf("failed to write build context file: %w", err)
	}
	return nil
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
	return nil
}

func (r *Reloader) reload() (e error) {
	if err := r.stopCurrentProcess(); err != nil {
		return xerrors.Errorf("failed to stop current process: %w", err)
	}
	execCmd := NewCommand(buildPath)
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

func (r *Reloader) buildOnHostOS() error {
	ctx, err := r.loadBuildContext()
	if err != nil {
		return xerrors.Errorf("failed to get build context: %w", err)
	}
	cmd := NewCommand("go", "build", "-o", buildPath, ".")
	cmd.AddEnv(ctx.toEnv())
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to command: %w", err)
	}
	return nil
}

func (r *Reloader) execCommandOnDockerContainer(ctx context.Context, containerName string, command []string) error {
	cli, err := client.NewEnvClient()
	if err != nil {
		return xerrors.Errorf("failed to create docker client: %w", err)
	}
	cfg := types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          command,
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
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	if _, err = stdcopy.StdCopy(stdout, stderr, attachResp.Reader); err != nil {
		return xerrors.Errorf("failed to copy stdout/stderr: %w", err)
	}
	fmt.Printf(stdout.String())
	fmt.Printf(stderr.String())
	return nil
}

func (r *Reloader) sendReloadingSignal() error {
	if r.host != nil && r.host.Docker != "" {
		pid, err := r.readPID()
		if err != nil {
			return xerrors.Errorf("failed to read pid: %w", err)
		}
		containerName := r.host.Docker
		command := []string{"kill", "-HUP", fmt.Sprint(pid)}
		if err := r.execCommandOnDockerContainer(context.Background(), containerName, command); err != nil {
			return xerrors.Errorf("failed to exec command on docker container: %w", err)
		}
		return nil
	}
	return nil
}
