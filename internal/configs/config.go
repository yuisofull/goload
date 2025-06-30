package configs

import (
	"github.com/yuisofull/goload/configs"
	"gopkg.in/yaml.v3"
)

type Config struct {
	MySQL               MySQL               `yaml:"mysql"`
	Redis               Redis               `yaml:"redis"`
	Auth                Auth                `yaml:"auth"`
	APIGateway          APIGateway          `yaml:"apigateway"`
	AuthService         AuthService         `yaml:"authservice"`
	DownloadTaskService DownloadTaskService `yaml:"downloadtaskservice"`
	FileService         FileService         `yaml:"fileservice"`
}

func Load() (*Config, error) {
	config := &Config{}
	configBytes := configs.DefaultConfigBytes
	if err := yaml.Unmarshal(configBytes, config); err != nil {
		return nil, err
	}
	return config, nil
}
