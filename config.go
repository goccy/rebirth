package rebirth

import (
	"io/ioutil"
	"os"

	"github.com/goccy/go-yaml"
	"golang.org/x/xerrors"
)

type Config struct {
	Host  *Host            `yaml:"host,omitempty"`
	Build *Build           `yaml:"build,omitempty"`
	Run   *Run             `yaml:"run,omitempty"`
	Watch *Watch           `yaml:"watch,omitempty"`
	Task  map[string]*Task `yaml:"task,omitempty"`
}

type Host struct {
	Docker string `yaml:"docker,omitempty"`
}

type Build struct {
	Main   string            `yaml:"main,omitempty"`
	Env    map[string]string `yaml:"env,omitempty"`
	Init   []string          `yaml:"init,omitempty"`
	Before []string          `yaml:"before,omitempty"`
	After  []string          `yaml:"after,omitempty"`
}

type Run struct {
	Env map[string]string `yaml:"env,omitempty"`
}

type Watch struct {
	Root   string   `yaml:"root,omitempty"`
	Ignore []string `yaml:"ignore,omitempty"`
}

type Task struct {
	Desc     string   `yaml:"desc,omitempty"`
	Commands []string `yaml:"commands,omitempty"`
}

func LoadConfig(confPath string) (*Config, error) {
	file, err := ioutil.ReadFile(confPath)
	if err != nil {
		return nil, xerrors.Errorf("failed to read config file from %s: %w", confPath, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(file, &cfg); err != nil {
		return nil, xerrors.New(yaml.FormatError(err, true, true))
	}
	return &cfg, nil
}

func ExistsConfig() bool {
	_, err := os.Stat(configDir)
	return err == nil
}
