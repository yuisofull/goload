package configs

type APIGateway struct {
	HTTP HTTPConfig `yaml:"http"`
	// TokenHMACSecret is the secret used to HMAC server-issued tokens.
	TokenHMACSecret string `yaml:"token_hmac_secret"`
	// Storage config for optional direct download backend (e.g. MinIO)
	Storage struct {
		Minio struct {
			Endpoint  string `yaml:"endpoint"`
			AccessKey string `yaml:"access_key"`
			SecretKey string `yaml:"secret_key"`
			Bucket    string `yaml:"bucket"`
			UseSSL    bool   `yaml:"use_ssl"`
		} `yaml:"minio"`
	} `yaml:"storage"`
}
