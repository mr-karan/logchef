package config

import (
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Server struct {
		Port int    `koanf:"port"`
		Host string `koanf:"host"`
	} `koanf:"server"`
	SQLite struct {
		Path string `koanf:"path"`
	} `koanf:"sqlite"`
}

type ClickhouseConfig struct {
	Host        string
	Port        int
	Database    string
	Username    string
	Password    string
	DialTimeout time.Duration
}

func Load() (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider("config.toml"), toml.Parser()); err != nil {
		return nil, err
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
