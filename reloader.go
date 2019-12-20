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

var (
	cwd               string
	configDir         string
	buildPath         string
	pidPath           string
	dockerRebirthPath string
	binPath           string
	srcPath           string
	pkgPath           string
)

func init() {
	cwd, _ = os.Getwd()
	configDir = ".rebirth"
	buildPath = filepath.Join(cwd, configDir, "program")
	pidPath = filepath.Join(configDir, "server.pid")
	dockerRebirthPath = filepath.Join(configDir, "__rebirth")
	binPath = filepath.Join(configDir, "bin")
	srcPath = filepath.Join(configDir, "src")
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

func (r *Reloader) Test(args []string) error {
	if err := r.xtest(args); err != nil {
		return xerrors.Errorf("failed to xtest: %w", err)
	}
	return nil
}

func (r *Reloader) Build(args []string) error {
	env, err := r.buildEnv()
	if err != nil {
		return xerrors.Errorf("failed to get build env: %w", err)
	}
	symlinkPath, err := r.getOrCreateSymlink()
	if err != nil {
		return xerrors.Errorf("failed to get symlink path: %w", err)
	}
	buildArgs := []string{
		"go",
		"build",
		"--ldflags",
		`-linkmode external -extldflags "-static"`,
	}
	buildArgs = append(buildArgs, args...)
	cmd := NewCommand(buildArgs...)
	gopath, err := filepath.Abs(configDir)
	if err != nil {
		return xerrors.Errorf("failed to get absolute path from %s: %w", configDir, err)
	}
	env = append(env, fmt.Sprintf("GOPATH=%s", gopath))
	cmd.AddEnv(env)
	cmd.SetDir(symlinkPath)
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to command: %w", err)
	}
	return nil
}

func (r *Reloader) GoRun(args []string) error {
	if r.isUsedDocker() && !r.isOnDockerContainer() {
		tmpfile, err := ioutil.TempFile(configDir, "script")
		if err != nil {
			return xerrors.Errorf("failed to create temporary file: %w", err)
		}
		defer os.Remove(tmpfile.Name())
		buildArgs := []string{
			"go",
			"build",
			"-o",
			tmpfile.Name(),
			"--ldflags",
			`-linkmode external -extldflags "-static"`,
		}
		buildArgs = append(buildArgs, args...)
		if err := r.execCommand(buildArgs...); err != nil {
			return xerrors.Errorf("failed to execute command: %w", err)
		}
		containerName := r.host.Docker
		cmd := []string{tmpfile.Name()}
		cmd = append(cmd, args...)
		if err := r.execCommandOnDockerContainerWithStd(context.Background(), containerName, cmd); err != nil {
			return xerrors.Errorf("failed to exec command on docker container: %w", err)
		}
		return nil
	}
	cmdArgs := []string{
		"go",
		"run",
		"--ldflags",
		`-linkmode external -extldflags "-static"`,
	}
	cmdArgs = append(cmdArgs, args...)
	if err := r.execCommand(cmdArgs...); err != nil {
		return xerrors.Errorf("failed to execute command: %w", err)
	}
	return nil
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
	if err := r.xbuildWithDir(filepath.Join(cwd, dockerRebirthPath), cmdFile, r.rebirthDir()); err != nil {
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

func (r *Reloader) getModulePath() string {
	if existsGoMod() {
		file, _ := ioutil.ReadFile(goModPath)
		return parseModulePath(file)
	}
	return ""
}

func (r *Reloader) xbuildWithDir(target, source, dir string) error {
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
	cmd.SetDir(dir)
	cmd.AddEnv(env)
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to command: %w", err)
	}
	return nil
}

func (r *Reloader) getOrCreateSymlink() (string, error) {
	modpath := r.getModulePath()
	if modpath == "" {
		curpath, err := filepath.Abs(".")
		if err != nil {
			return "", xerrors.Errorf("failed to get abolute path from current dir: %w", err)
		}
		modpath = filepath.Base(curpath)
	}
	if err := os.MkdirAll(filepath.Join(srcPath, filepath.Dir(modpath)), 0755); err != nil {
		return "", xerrors.Errorf("failed to create path to %s: %w", modpath, err)
	}
	symlinkPath := filepath.Join(srcPath, modpath)
	if _, err := os.Stat(symlinkPath); err != nil {
		oldpath, err := filepath.Abs(".")
		if err != nil {
			return "", xerrors.Errorf("failed to get abolute path from current dir: %w", err)
		}
		newpath, err := filepath.Abs(symlinkPath)
		if err != nil {
			return "", xerrors.Errorf("failed to get abolute path from %s: %w", symlinkPath, err)
		}
		if err := os.Symlink(oldpath, newpath); err != nil {
			return "", xerrors.Errorf("failed to create symlink from %s to %s: %w", oldpath, newpath, err)
		}
	}
	return symlinkPath, nil
}

func (r *Reloader) execCommand(args ...string) error {
	env, err := r.buildEnv()
	if err != nil {
		return xerrors.Errorf("failed to get build env: %w", err)
	}
	symlinkPath, err := r.getOrCreateSymlink()
	if err != nil {
		return xerrors.Errorf("failed to get symlink path: %w", err)
	}
	cmd := NewCommand(args...)
	gopath, err := filepath.Abs(configDir)
	if err != nil {
		return xerrors.Errorf("failed to get absolute path from %s: %w", configDir, err)
	}
	env = append(env, fmt.Sprintf("GOPATH=%s", gopath))
	cmd.AddEnv(env)
	cmd.SetDir(symlinkPath)
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to command: %w", err)
	}
	return nil
}

func (r *Reloader) xtest(args []string) error {
	if r.isUsedDocker() && !r.isOnDockerContainer() {
		cmdArgs := []string{
			"go",
			"test",
			"-c",
			"-o",
			filepath.Join(configDir, "app.test"),
			"--ldflags",
			`-linkmode external -extldflags "-static"`,
		}
		cmdArgs = append(cmdArgs, args...)
		if err := r.execCommand(cmdArgs...); err != nil {
			return xerrors.Errorf("failed to execute command: %w", err)
		}
		containerName := r.host.Docker
		cmd := []string{filepath.Join(configDir, "app.test")}
		testArgs := []string{}
		for idx, arg := range args {
			switch arg {
			case "-v":
				testArgs = append(testArgs, "-test.v")
			case "-run":
				testArgs = append(testArgs, "-test.run", args[idx+1])
			}
		}
		cmd = append(cmd, testArgs...)
		fmt.Println("cmd = ", cmd)
		if err := r.execCommandOnDockerContainerWithStd(context.Background(), containerName, cmd); err != nil {
			return xerrors.Errorf("failed to exec command on docker container: %w", err)
		}
		return nil
	}
	cmdArgs := []string{
		"go",
		"test",
		"--ldflags",
		`-linkmode external -extldflags "-static"`,
	}
	cmdArgs = append(cmdArgs, args...)
	if err := r.execCommand(cmdArgs...); err != nil {
		return xerrors.Errorf("failed to execute command: %w", err)
	}
	return nil
}

func (r *Reloader) xbuild(target, source string) error {
	env, err := r.buildEnv()
	if err != nil {
		return xerrors.Errorf("failed to get build env: %w", err)
	}
	symlinkPath, err := r.getOrCreateSymlink()
	if err != nil {
		return xerrors.Errorf("failed to get symlink path: %w", err)
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
	gopath, err := filepath.Abs(configDir)
	if err != nil {
		return xerrors.Errorf("failed to get absolute path from %s: %w", configDir, err)
	}
	env = append(env, fmt.Sprintf("GOPATH=%s", gopath))
	cmd.AddEnv(env)
	cmd.SetDir(symlinkPath)
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
	env := []string{
		fmt.Sprintf("PATH=%s:%s", os.Getenv("PATH"), filepath.Join(r.rebirthDir(), "bin")),
		"CGO_ENABLED=1",
		fmt.Sprintf("GOOS=%s", goos),
		fmt.Sprintf("GOARCH=%s", goarch),
	}
	if r.build != nil {
		for k, v := range r.build.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	if r.isUsedDocker() && runtime.GOOS == "darwin" {
		env = append(env, []string{
			"CC=x86_64-linux-musl-cc",
			"CXX=x86_64-linux-musl-c++",
		}...)
	}
	return env, nil
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

func (r *Reloader) execRebirthOnDockerContainer(ctx context.Context, containerName string) error {
	if err := r.execCommandOnDockerContainerWithStd(ctx, containerName, []string{dockerRebirthPath}); err != nil {
		return xerrors.Errorf("failed to execute command on docker container: %w", err)
	}
	return nil
}

func (r *Reloader) execCommandOnDockerContainerWithStd(ctx context.Context, containerName string, command []string) error {
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
	if err := r.reload(); err != nil {
		return xerrors.Errorf("failed to reload: %w", err)
	}
	return nil
}
