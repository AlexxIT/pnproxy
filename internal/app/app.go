package app

import (
	"flag"
	"net/url"
	"strings"
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

	Info["version"] = Version
	Info["config_path"] = configPath
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
