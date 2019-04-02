package main

import (
	"flag"
	"log"
	"net/http"

	otn "github.com/opentradingnetworkfoundation/otn-go/otn-microservice"

	"github.com/gorilla/mux"

	"github.com/opentradingnetworkfoundation/otn-go/httpserver"
	"github.com/opentradingnetworkfoundation/otn-go/secrets"
	"github.com/opentradingnetworkfoundation/otn-go/wallet"

	"github.com/juju/errors"

	"go.uber.org/dig"
)

func NewWallet(cfg *faucetConfig) (wallet.Wallet, error) {
	wallet := wallet.NewWallet()

	if cfg.Secrets != nil {
		stg, err := secrets.NewSecretStorage(cfg.Secrets)
		if err != nil {
			return nil, errors.Annotate(err, "create secret storage")
		}

		keys, err := stg.ReadStringArray("account")
		if err != nil {
			return nil, errors.Annotate(err, "get account keys")
		}

		cfg.Keys = append(cfg.Keys, keys...)
	}

	if err := wallet.AddPrivateKeys(cfg.Keys); err != nil {
		log.Printf("Unable to add wallet private keys: %v", err)
		return nil, err
	}

	return wallet, nil
}

func NewStarter(f *faucet, cfg *faucetConfig) *otn.Starter {
	return otn.NewStarter(f, &otn.StarterConfig{TrustedNode: cfg.TrustedNode})
}

func main() {
	cfgFile := flag.String("cfg", "/projects/otn/etc/otn-faucet.json", "Use this configuration file")
	flag.Parse()

	di := dig.New()

	di.Provide(func() *faucetConfig {
		cfg := &faucetConfig{}
		if err := loadConfig(*cfgFile, cfg); err != nil {
			log.Fatalln("Cannot load config:", err)
		}
		return cfg
	})

	di.Provide(mux.NewRouter)
	di.Provide(func(router *mux.Router) http.Handler { return router })
	di.Provide(func(cfg *faucetConfig) *httpserver.Config { return &cfg.HTTPServer })
	di.Provide(httpserver.NewHTTPServer)
	di.Provide(NewWallet)
	di.Provide(NewFaucet)
	di.Provide(NewStarter)

	err := di.Invoke(func(starter *otn.Starter, cfg *faucetConfig) {
		log.Printf("Using node %s; Serving on address %s", cfg.TrustedNode, cfg.HTTPServer.Addr)
		doneChan := make(chan struct{})
		starter.Run(doneChan)
	})

	if err != nil {
		log.Fatal(err)
	}
}
