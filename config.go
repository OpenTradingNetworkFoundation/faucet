package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/juju/errors"
	"go.uber.org/zap"

	"github.com/opentradingnetworkfoundation/otn-go/httpserver"
	"github.com/opentradingnetworkfoundation/otn-go/secrets"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(data []byte) (err error) {
	s := string(data)
	if s == "null" {
		return
	}

	s, err = strconv.Unquote(s)
	if err != nil {
		return
	}

	t, err := time.ParseDuration(s)
	if err != nil {
		return
	}

	*d = Duration(t)
	return
}

type RateLimiterConfig struct {
	Duration Duration `json:"duration"`
	Capacity int64    `json:"capacity"`
}

type faucetConfig struct {
	TrustedNode       string                 `json:"trusted_node"`
	HTTPServer        httpserver.Config      `json:"http_server"`
	Registrar         string                 `json:"registrar"`
	DefaultReferrer   string                 `json:"default_referrer"`
	ReferrerPercent   int                    `json:"referrer_percent"`
	Keys              []string               `json:"keys"`
	Secrets           *secrets.StorageConfig `json:"secrets"`
	RateLimiterConfig *RateLimiterConfig     `json:"ratelimit"`
	Logger            zap.Config             `json:"logger"`
}

func loadConfig(filename string, cfg *faucetConfig) error {
	// Read the config file.
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return errors.Annotatef(err, "Error reading file %s", filename)
	}

	cfg.Logger = zap.NewProductionConfig()
	// Decode json.
	if err = json.Unmarshal(data, cfg); err != nil {
		return err
	}
	cfg.TrustedNode = os.ExpandEnv(cfg.TrustedNode)
	return nil
}
