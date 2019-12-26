package rebirth

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/goccy/rebirth/internal/errors"
	"github.com/mitchellh/go-ps"
	"golang.org/x/xerrors"
)

type Command struct {
	cmd  *exec.Cmd
	args []string
}

func NewCommand(args ...string) *Command {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	return &Command{
		cmd:  cmd,
		args: args,
	}
}

func (c *Command) SetDir(dir string) {
	c.cmd.Dir = dir
}

func (c *Command) AddEnv(env []string) {
	c.cmd.Env = append(c.cmd.Env, env...)
}

func (c *Command) String() string {
	return fmt.Sprintf("%s; %s",
		strings.Join(c.cmd.Env, " "),
		strings.Join(c.args, " "),
	)
}

func (c *Command) Stop() error {
	if c == nil {
		return nil
	}
	if c.cmd == nil {
		return nil
	}
	if c.cmd.Process == nil {
		return nil
	}
	pid := c.cmd.Process.Pid
	process, err := ps.FindProcess(pid)
	if err != nil {
		return xerrors.Errorf("failed to find process by pid(%d): %w", pid, err)
	}
	if process != nil {
		if err := c.cmd.Process.Kill(); err != nil {
			return xerrors.Errorf("failed to kill process: %w", err)
		}
	}
	return nil
}

func (c *Command) Run() error {
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return xerrors.Errorf("failed to pipe stdout: %w", err)
	}
	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		return xerrors.Errorf("failed to pipe stderr: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		return xerrors.Errorf("failed to run build command: %w", err)
	}
	io.Copy(os.Stdout, stdout)
	errstream, err := ioutil.ReadAll(stderr)
	if err != nil {
		return xerrors.Errorf("failed to read from stderr: %w", err)
	}
	if err := c.cmd.Wait(); err != nil {
		return xerrors.New(string(errstream))
	}
	return nil
}

func (c *Command) runWithStdCopy() error {
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return xerrors.Errorf("failed to pipe stdout: %w", err)
	}
	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		return xerrors.Errorf("failed to pipe stderr: %w", err)
	}
	if err := c.cmd.Start(); err != nil {
		return xerrors.Errorf("failed to run build command: %w", err)
	}
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)
	if err := c.cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func (c *Command) RunAsync() {
	go func() {
		if err := c.runWithStdCopy(); err != nil {
			fmt.Println(err)
		}
	}()
}

type DockerCommand struct {
	container string
	cmd       []string
	execID    string
}

func NewDockerCommand(container string, cmd ...string) *DockerCommand {
	return &DockerCommand{
		container: container,
		cmd:       cmd,
	}
}

/*
type DockerProcess struct {
	Pid int
}

func (c *DockerCommand) Process() *DockerProcess {
	if c.execID == "" {
		return nil
	}
	cli, err := client.NewEnvClient()
	if err != nil {
		return nil
	}

	resp, err := cli.ContainerExecInspect(context.Background(), c.execID)
	if err != nil {
		return nil
	}
	// resp.Pid is number on host
	return &DockerProcess{
		Pid: resp.Pid,
	}
}
*/

func (c *DockerCommand) Output() ([]byte, error) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	if err := c.run(context.Background(), func(reader *bufio.Reader) error {
		if _, err := stdcopy.StdCopy(stdout, stderr, reader); err != nil {
			return xerrors.Errorf("failed to copy stdout/stderr: %w", err)
		}
		return nil
	}); err != nil {
		return nil, xerrors.Errorf("failed to run: %w", err)
	}
	return []byte(c.chomp(stdout.String())), nil
}

func (c *DockerCommand) Run() error {
	if err := c.run(context.Background(), func(reader *bufio.Reader) error {
		if _, err := stdcopy.StdCopy(os.Stdout, os.Stderr, reader); err != nil {
			return xerrors.Errorf("failed to copy stdout/stderr: %w", err)
		}
		return nil
	}); err != nil {
		return xerrors.Errorf("failed to run: %w", err)
	}
	return nil
}

func (c *DockerCommand) run(ctx context.Context, ioCallback func(reader *bufio.Reader) error) error {
	cli, err := client.NewEnvClient()
	if err != nil {
		return xerrors.Errorf("failed to create docker client: %w", err)
	}
	cfg := types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          c.cmd,
	}
	execResp, err := cli.ContainerExecCreate(ctx, c.container, cfg)
	if err != nil {
		return xerrors.Errorf("failed to ContainerExecCreate: %w", err)
	}
	execID := execResp.ID
	c.execID = execID
	attachResp, err := cli.ContainerExecAttach(ctx, execID, cfg)
	if err != nil {
		return xerrors.Errorf("failed to ContainerExecAttach: %w", err)
	}
	defer attachResp.Close()
	if err := ioCallback(attachResp.Reader); err != nil {
		return xerrors.Errorf("failed to i/o callback: %w", err)
	}
	return nil
}

func (c *DockerCommand) chomp(src string) string {
	return strings.TrimRight(src, "\n")
}

type GoCommand struct {
	cmd          []string
	container    string
	isCrossBuild bool
	extEnv       []string
	dir          string
}

func NewGoCommand() *GoCommand {
	return &GoCommand{
		extEnv: []string{},
	}
}

func (c *GoCommand) EnableCrossBuild(container string) {
	c.container = container
	c.isCrossBuild = true
}

func (c *GoCommand) AddEnv(env []string) {
	c.extEnv = append(c.extEnv, env...)
}

func (c *GoCommand) SetDir(dir string) {
	c.dir = dir
}

func (c *GoCommand) Build(args ...string) error {
	cmd := []string{"go", "build"}
	cmd = append(cmd, c.linkerFlags()...)
	cmd = append(cmd, args...)
	if err := c.run(cmd...); err != nil {
		return xerrors.Errorf("failed to run: %w", err)
	}
	return nil
}

func (c *GoCommand) Run(args ...string) error {
	if !c.isCrossBuild {
		cmd := []string{"go", "run"}
		cmd = append(cmd, c.linkerFlags()...)
		cmd = append(cmd, args...)
		if err := c.run(cmd...); err != nil {
			return xerrors.Errorf("failed to run: %w", err)
		}
		return nil
	}

	tmpfile, err := ioutil.TempFile(configDir, "script")
	if err != nil {
		return xerrors.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpfile.Name())
	cmd := []string{"go", "build", "-o", tmpfile.Name()}
	cmd = append(cmd, c.linkerFlags()...)
	cmd = append(cmd, args...)
	if err := c.run(cmd...); err != nil {
		return xerrors.Errorf("failed to run: %w", err)
	}
	dockerCmd := []string{tmpfile.Name()}
	dockerCmd = append(dockerCmd, args...)
	if err := NewDockerCommand(c.container, dockerCmd...).Run(); err != nil {
		return xerrors.Errorf("failed to run on docker container: %w", err)
	}
	return nil
}

func (c *GoCommand) Test(args ...string) error {
	if !c.isCrossBuild {
		cmd := []string{"go", "test"}
		cmd = append(cmd, c.linkerFlags()...)
		cmd = append(cmd, args...)
		if err := c.run(cmd...); err != nil {
			return xerrors.Errorf("failed to run: %w", err)
		}
		return nil
	}

	cmd := []string{"go", "test", "-c", "-o", filepath.Join(configDir, "app.test")}
	cmd = append(cmd, c.linkerFlags()...)
	cmd = append(cmd, args...)
	if err := c.run(cmd...); err != nil {
		return xerrors.Errorf("failed to run: %w", err)
	}
	dockerCmd := []string{filepath.Join(configDir, "app.test")}
	testArgs := []string{}
	for idx, arg := range args {
		switch arg {
		case "-v":
			testArgs = append(testArgs, "-test.v")
		case "-run":
			testArgs = append(testArgs, "-test.run", args[idx+1])
		}
	}
	dockerCmd = append(dockerCmd, testArgs...)
	if err := NewDockerCommand(c.container, dockerCmd...).Run(); err != nil {
		return xerrors.Errorf("failed to run on docker container: %w", err)
	}
	return nil
}

func (c *GoCommand) linkerFlags() []string {
	if c.isCrossBuild {
		return []string{
			"--ldflags",
			`-linkmode external -extldflags "-static"`,
		}
	}
	return []string{}
}

func (c *GoCommand) run(args ...string) error {
	env, err := c.buildEnv()
	if err != nil {
		return xerrors.Errorf("failed to get build env: %w", err)
	}
	cmd := NewCommand(args...)
	if c.dir == "" {
		symlinkPath, err := c.getOrCreateSymlink()
		if err != nil {
			return xerrors.Errorf("failed to get symlink path: %w", err)
		}
		gopath, err := c.gopath()
		if err != nil {
			return xerrors.Errorf("failed to get GOPATH: %w", err)
		}
		env = append(env, fmt.Sprintf("GOPATH=%s", gopath))
		cmd.SetDir(symlinkPath)
	} else {
		cmd.SetDir(c.dir)
	}
	cmd.AddEnv(env)
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("failed to command: %w", err)
	}
	return nil
}

func (c *GoCommand) buildEnv() ([]string, error) {
	goos, err := c.buildGOOS()
	if err != nil {
		return nil, xerrors.Errorf("failed to get GOOS for build: %w", err)
	}
	goarch, err := c.buildGOARCH()
	if err != nil {
		return nil, xerrors.Errorf("failed to get GOARCH for build: %w", err)
	}
	env := []string{
		"CGO_ENABLED=1",
		fmt.Sprintf("GOOS=%s", goos),
		fmt.Sprintf("GOARCH=%s", goarch),
	}
	env = append(env, c.extEnv...)
	if c.isCrossBuild && runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("x86_64-linux-musl-cc"); err != nil {
			return nil, errors.ErrCrossCompiler
		}
		env = append(env, []string{
			"CC=x86_64-linux-musl-cc",
			"CXX=x86_64-linux-musl-c++",
		}...)
	}
	return env, nil
}

func (c *GoCommand) buildGOOS() (string, error) {
	if c.isCrossBuild {
		goos, err := NewDockerCommand(c.container, "go", "env", "GOOS").Output()
		if err != nil {
			return "", xerrors.Errorf("failed to get GOOS env on container: %w", err)
		}
		return string(goos), nil
	}
	return runtime.GOOS, nil
}

func (c *GoCommand) buildGOARCH() (string, error) {
	if c.isCrossBuild {
		goarch, err := NewDockerCommand(c.container, "go", "env", "GOARCH").Output()
		if err != nil {
			return "", xerrors.Errorf("failed to get GOARCH env on container: %w", err)
		}
		return string(goarch), nil
	}
	return runtime.GOARCH, nil
}

func (c *GoCommand) gopath() (string, error) {
	path, err := filepath.Abs(configDir)
	if err != nil {
		return "", xerrors.Errorf("failed to get absolute path from %s: %w", configDir, err)
	}
	return path, nil
}

func (c *GoCommand) srcPath() (string, error) {
	gopath, err := c.gopath()
	if err != nil {
		return "", xerrors.Errorf("failed to get GOPATH: %w", err)
	}
	return filepath.Join(gopath, "src"), nil
}

func (c *GoCommand) getOrCreateSymlink() (string, error) {
	modpath, err := c.getModulePath()
	if err != nil {
		return "", xerrors.Errorf("failed to get module path: %w", err)
	}
	srcPath, err := c.srcPath()
	if err != nil {
		return "", xerrors.Errorf("failed to get $GOPATH/src path: %w", err)
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

func (c *GoCommand) getModulePath() (string, error) {
	if existsGoMod() {
		file, _ := ioutil.ReadFile(goModPath)
		return parseModulePath(file), nil
	}
	curpath, err := filepath.Abs(".")
	if err != nil {
		return "", xerrors.Errorf("failed to get abolute path from current dir: %w", err)
	}
	return filepath.Base(curpath), nil
}
