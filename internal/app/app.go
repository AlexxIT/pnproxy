package app

import (
	"flag"
	"net/url"
	"strings"
)

func Init() {
	var configName string

	flag.StringVar(&configName, "config", "pnproxy.yaml", "Path to config file")
	flag.Parse()

	initConfig(configName)
	initLog()
}

func ParseAction(raw string) (action string, params url.Values) {
	fields := strings.Fields(raw)

	if len(fields) > 0 {
		action = fields[0]
		params = url.Values{}
		for i := 1; i < len(fields); i += 2 {
			k := fields[i]
			v := fields[i+1]
			params[k] = append(params[k], v)
		}
	}

	return
}
