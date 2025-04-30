package config

import (
	"fmt"
	"time"

	"balancer/internal/domain/errors"

	"github.com/spf13/viper"
)

type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Backends      []BackendConfig     `mapstructure:"backends"`
	LoadBalancing LoadBalancingConfig `mapstructure:"loadbalancing"`
	RateLimiting  RateLimitingConfig  `mapstructure:"rateLimiting"`
	Database      DatabaseConfig      `mapstructure:"database"`
	Logging       LoggingConfig       `mapstructure:"logging"`
}

type ServerConfig struct {
	Port              int           `mapstructure:"port"`
	ReadTimeout       time.Duration `mapstructure:"readTimeout"`
	WriteTimeout      time.Duration `mapstructure:"writeTimeout"`
	ReadHeaderTimeout time.Duration `mapstructure:"readHeaderTimeout"`
}

type BackendConfig struct {
	URL    string `mapstructure:"url"`
	Weight int    `mapstructure:"weight"`
}

type LoadBalancingConfig struct {
	Algorithm           string        `mapstructure:"algorithm"`
	HealthCheckInterval time.Duration `mapstructure:"healthCheckInterval"`
	HealthCheckTimeout  time.Duration `mapstructure:"healthCheckTimeout"`
}

type RateLimitingConfig struct {
	DefaultCapacity      int     `mapstructure:"defaultCapacity"`
	DefaultRatePerSecond float64 `mapstructure:"defaultRatePerSecond"`
	Enabled              bool    `mapstructure:"enabled"`
	StorageType          string  `mapstructure:"storageType"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

func LoadConfig(path string) (*Config, error) {
	v := viper.New()

	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, &errors.ErrConfigFileNotFound{FileName: path}
		}

		return nil, &errors.ErrConfigFileParseError{Message: err.Error()}
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, &errors.ErrConfigFileParseError{Message: fmt.Sprintf("ошибка при разборе конфигурации: %v", err)}
	}

	return &config, nil
}
