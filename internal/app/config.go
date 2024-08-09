package app

import (
	"os"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

func LoadConfig(v any) {
	if err := yaml.Unmarshal(config, v); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}

var config []byte

func initConfig(fileName string) {
	var err error
	if config, err = os.ReadFile(fileName); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}
