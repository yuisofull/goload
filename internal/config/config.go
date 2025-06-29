package config

import (
	"github.com/kelseyhightower/envconfig"
	"github.com/yuisofull/goload/configs"
	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTP struct {
		Address string `yaml:"address" envconfig:"HTTP_ADDRESS"`
	}
	GRPC struct {
		Address string `yaml:"address" envconfig:"GRPC_ADDRESS"`
	}
	Auth struct {
		GRPC struct {
			Address string `yaml:"address" envconfig:"AUTH_GRPC_ADDRESS"`
		}
	}
	DownloadTask struct {
		GRPC struct {
			Address string `yaml:"address" envconfig:"DOWNLOAD_TASK_GRPC_ADDRESS"`
		}
	}
	File struct {
		GRPC struct {
			Address string `yaml:"address" envconfig:"FILE_GRPC_ADDRESS"`
		}
	}
}

func Load() (*Config, error) {
	config := &Config{}
	err := yaml.Unmarshal(configs.DefaultConfigBytes, config)
	if err != nil {
		return nil, err
	}
	err = envconfig.Process("", config)
	if err != nil {
		return nil, err
	}
	return config, nil
}
