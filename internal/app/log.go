package app

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func initLog() {
	var cfg struct {
		Log struct {
			Level string `yaml:"level"`
		} `yaml:"log"`
	}

	cfg.Log.Level = "info"

	LoadConfig(&cfg)

	lvl, err := zerolog.ParseLevel(cfg.Log.Level)
	if err != nil {
		log.Warn().Err(err).Caller().Send()
		return
	}

	log.Logger = log.Logger.Level(lvl)

	zerolog.TimeFieldFormat = time.RFC3339Nano
}
