package proxy

import (
	"net/http"

	"github.com/AlexxIT/pnproxy/internal/app"
	ihttp "github.com/AlexxIT/pnproxy/internal/http"
	"github.com/AlexxIT/pnproxy/internal/tls"
	"github.com/rs/zerolog/log"
)

func Init() {
	var cfg struct {
		Proxy struct {
			Listen string `yaml:"listen"`
		} `yaml:"proxy"`
	}

	app.LoadConfig(&cfg)

	if cfg.Proxy.Listen != "" {
		go serve(cfg.Proxy.Listen)
	}
}

func serve(address string) {
	log.Info().Msgf("[proxy] listen=%s", address)
	srv := &http.Server{
		Addr:    address,
		Handler: http.HandlerFunc(Handle),
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}

func Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		src, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}
		if _, err = src.Write([]byte("HTTP/1.0 200 Connection established\r\n\r\n")); err != nil {
			log.Warn().Err(err).Caller().Send()
			return
		}
		tls.Handle(src)
	} else {
		r.RequestURI = ""

		ihttp.Handle(w, r)
	}
}
