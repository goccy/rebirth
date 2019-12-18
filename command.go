package rebirth

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

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
	if err := c.cmd.Process.Kill(); err != nil {
		return xerrors.Errorf("failed to kill process: %w", err)
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
