package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	MongoURL string
}

type saymonConfig struct {
	DB struct {
		MongoDB struct {
			URL string `json:"url"`
		} `json:"mongodb"`
	} `json:"db"`
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file %q: %w", path, err)
	}

	var raw saymonConfig
	if err := json.Unmarshal(b, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config json: %w", err)
	}

	if raw.DB.MongoDB.URL == "" {
		return Config{}, fmt.Errorf("mongodb.url is empty in %q", path)
	}

	return Config{
		MongoURL: raw.DB.MongoDB.URL,
	}, nil
}
