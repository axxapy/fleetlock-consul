package internal

import (
	"fmt"
	"github.com/hashicorp/go-sockaddr/template"
	"github.com/kelseyhightower/envconfig"
)

type (
	Config struct {
		//LockTimeout  time.Duration    `yaml:"lock_timeout" envconfig:"lock_timeout" default:"15s"`
		DefaultGroup string           `yaml:"default_group" envconfig:"default_group" default:"default"`
		Consul       ConsulConfig     `yaml:"consul" envconfig:"consul"`
		Http         HttpServerConfig `yaml:"http" envconfig:"http"`
	}

	HttpServerConfig struct {
		Listen string `yaml:"listen" envconfig:"listen" default:"{{ GetPrivateIP }}:9090, 127.0.0.1:9090"`
	}

	ConsulConfig struct {
		Address string `yaml:"address" envconfig:"address" default:"127.0.0.1:8500"`
		Auth    string `yaml:"auth" envconfig:"auth"`
		Token   string `yaml:"token" envconfig:"token"`
	}
)

func ParseConfig() (Config, error) {
	var c Config
	err := envconfig.Process("FLEETLOCK", &c)
	if err != nil {
		return Config{}, err
	}

	results, err := template.Parse(c.Http.Listen)
	if err != nil {
		return Config{}, fmt.Errorf("failed to parse http listen address: %w", err)
	}
	c.Http.Listen = results

	return c, nil
}
