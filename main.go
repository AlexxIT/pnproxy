package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/AlexxIT/pnproxy/internal/app"
	"github.com/AlexxIT/pnproxy/internal/dns"
	"github.com/AlexxIT/pnproxy/internal/hosts"
	"github.com/AlexxIT/pnproxy/internal/http"
	"github.com/AlexxIT/pnproxy/internal/proxy"
	"github.com/AlexxIT/pnproxy/internal/tls"
)

func main() {
	app.Init()   // before all
	hosts.Init() // before others

	dns.Init()
	tls.Init()
	http.Init()
	proxy.Init()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	println("exit with signal:", (<-sigs).String())
}
