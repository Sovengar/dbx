package config

import (
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type ProjectConfig struct {
	Connections map[string]ProjectConnection `toml:"connections"`
}

type ProjectConnection struct {
	Driver    string `toml:"driver"`
	DSN       string `toml:"dsn"`
	SSHTunnel string `toml:"ssh_tunnel,omitempty"`
}

type FoundProject struct {
	Name       string
	Path       string
	Connection ProjectConnection
	Active     bool
}

func LoadProjectConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &ProjectConfig{}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func expandEnv(s string) string {
	if strings.HasPrefix(s, "${env:") && strings.HasSuffix(s, "}") {
		envName := s[6 : len(s)-1]
		if val := os.Getenv(envName); val != "" {
			return val
		}
	}
	return os.ExpandEnv(s)
}

func (c *ProjectConnection) GetDSN() string {
	return expandEnv(c.DSN)
}
