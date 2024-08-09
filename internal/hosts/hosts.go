package hosts

import (
	"strings"

	"github.com/AlexxIT/pnproxy/internal/app"
)

func Init() {
	var cfg struct {
		Hosts map[string]string `yaml:"hosts"`
	}

	app.LoadConfig(&cfg)

	for alias, aliases := range cfg.Hosts {
		hosts[alias] = Get(aliases)
	}
}

// Get convert list of aliases and domains to domains
func Get(aliases string) (domains []string) {
	for _, alias := range strings.Fields(aliases) {
		if names, ok := hosts[alias]; ok {
			domains = append(domains, names...)
		} else {
			domains = append(domains, alias)
		}
	}
	return
}

var hosts = map[string][]string{}
