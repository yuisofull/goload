package configs

import (
	"github.com/yuisofull/goload/configs"
	"gopkg.in/yaml.v3"
	"os"
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

func Load(configFilePath string) (*Config, error) {
	var configBytes []byte
	var err error
	config := &Config{}

	if configFilePath != "" {
		if configBytes, err = os.ReadFile(configFilePath); err != nil {
			return nil, err
		}
	} else {
		configBytes = configs.DefaultConfigBytes
	}
	if err := yaml.Unmarshal(configBytes, config); err != nil {
		return nil, err
	}
	return config, nil
}
