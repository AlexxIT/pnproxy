package app

import (
	"flag"
	"net/url"
	"runtime/debug"
	"strings"

	"github.com/rs/zerolog/log"
)

var (
	Version string
	Info    = make(map[string]any)
)

func Init() {
	var configPath string

	flag.StringVar(&configPath, "config", "pnproxy.yaml", "Path to config file")
	flag.Parse()

	initConfig(configPath)
	initLog()
	initVersion()

	Info["version"] = Version
	Info["config_path"] = configPath

	log.Info().Str("version", Version).Msg("pnproxy")
}

func ParseAction(raw string) (fields []string, params url.Values) {
	fields = strings.Fields(raw)
	params = url.Values{}
	for i := 1; i+1 < len(fields); i += 2 {
		k := fields[i]
		v := fields[i+1]
		params[k] = append(params[k], v)
	}
	return
}

func initVersion() {
	if info, ok := debug.ReadBuildInfo(); ok {
		var revision string

		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if len(setting.Value) > 7 {
					revision = setting.Value[:7]
				} else {
					revision = setting.Value
				}
			case "vcs.modified":
				if setting.Value == "true" {
					revision += ".dirty"
				}
			}
		}

		if info.Main.Version != "v"+Version {
			Version += "+dev." + revision
		}
	}
}
