package config

import (
	"github.com/yuisofull/goload/configs"
	"gopkg.in/yaml.v3"
	"os"
)

type Config struct {
	MySQL               MySQLConfig               `yaml:"mysql"`
	Redis               RedisConfig               `yaml:"redis"`
	Auth                AuthConfig                `yaml:"auth"`
	APIGateway          APIGatewayConfig          `yaml:"apigateway"`
	AuthService         AuthServiceConfig         `yaml:"authservice"`
	DownloadTaskService DownloadTaskServiceConfig `yaml:"downloadtaskservice"`
	FileService         FileServiceConfig         `yaml:"fileservice"`
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
