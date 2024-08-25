package dns

import "net/url"

// https://ru.wikipedia.org/wiki/DNS_%D0%BF%D0%BE%D0%B2%D0%B5%D1%80%D1%85_HTTPS
// https://ru.wikipedia.org/wiki/DNS_%D0%BF%D0%BE%D0%B2%D0%B5%D1%80%D1%85_TLS
var providers = map[string]string{
	"cloudflare": "1.1.1.1",
	"google":     "8.8.8.8",
	"quad9":      "9.9.9.9",
	"opendns":    "208.67.222.222",
	"yandex":     "77.88.8.8",
}

func server(params url.Values) string {
	if params.Has("provider") {
		return providers[params.Get("provider")]
	}
	return params.Get("server")
}
