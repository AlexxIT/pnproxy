package api

import (
	"encoding/json"
	"net/http"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/rs/zerolog/log"
)

func Init() {
	var cfg struct {
		API struct {
			Listen string `yaml:"listen"`
		} `yaml:"api"`
	}

	app.LoadConfig(&cfg)

	if cfg.API.Listen == "" {
		return
	}

	http.HandleFunc("GET /api", api)
	http.HandleFunc("GET /api/request", apiRequest)
	http.HandleFunc("GET /api/stack", apiStack)

	go serve(cfg.API.Listen)
}

func serve(address string) {
	log.Info().Msgf("[api] listen=%s", address)

	srv := &http.Server{Addr: address}
	if err := srv.ListenAndServe(); err != nil {
		log.Error().Err(err).Caller().Send()
	}
}

func api(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(app.Info)
}
