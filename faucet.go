package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/opentradingnetworkfoundation/otn-go/api"
	"github.com/opentradingnetworkfoundation/otn-go/objects"
	"github.com/opentradingnetworkfoundation/otn-go/wallet"

	"github.com/gorilla/mux"
	"github.com/juju/ratelimit"
)

func createKeyAuthority(pubKey string) objects.Authority {
	pk := objects.NewPublicKey(pubKey)
	auth := objects.Authority{
		AccountAuths:    objects.MapAccountAuths{},
		KeyAuths:        objects.MapKeyAuths{},
		WeightThreshold: 1,
		Extensions:      objects.Extensions{},
	}

	auth.KeyAuths[pk] = 1
	return auth
}

type accountInfo struct {
	Name      string `json:"name"`
	ActiveKey string `json:"active_key"`
	OwnerKey  string `json:"owner_key"`
	MemoKey   string `json:"memo_key"`
	Referrer  string `json:"referrer,omitempty"`
	Refcode   string `json:"refcode,omitempty"`
}

type faucet struct {
	cfg         *faucetConfig
	log         *zap.SugaredLogger
	wallet      wallet.Wallet
	httpServer  *http.Server
	router      *mux.Router
	rpc         api.BitsharesAPI
	rateLimiter *RateLimiter

	registrar       *objects.Account
	defaultReferrer *objects.Account
}

func createRateLimiter(cfg *RateLimiterConfig) *RateLimiter {
	if cfg == nil {
		return nil
	}
	return NewRateLimiter(func() *ratelimit.Bucket {
		return ratelimit.NewBucket(time.Duration(cfg.Duration), cfg.Capacity)
	})
}

func setRateLimitHeaders(w http.ResponseWriter, cfg *RateLimiterConfig, remoteIP string) {
	w.Header().Add("X-Rate-Limit-Limit", fmt.Sprintf("%d", cfg.Capacity))
	w.Header().Add("X-Rate-Limit-Duration", fmt.Sprintf("%.2f", time.Duration(cfg.Duration).Seconds()))
	w.Header().Add("X-Rate-Limit-Request-Remote-Addr", remoteIP)
}

func writeJSONResponse(w http.ResponseWriter, statusCode int, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return err
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	w.Write(data)
	return nil
}

func NewFaucet(
	cfg *faucetConfig,
	server *http.Server,
	router *mux.Router,
	wallet wallet.Wallet) (*faucet, error) {

	l, err := cfg.Logger.Build()
	if err != nil {
		return nil, err
	}
	f := &faucet{
		cfg:         cfg,
		log:         l.Sugar(),
		wallet:      wallet,
		httpServer:  server,
		router:      router,
		rateLimiter: createRateLimiter(cfg.RateLimiterConfig),
	}

	f.router.HandleFunc("/api/v1/accounts", f.accounts).Methods("POST")
	f.router.HandleFunc("/api/v1/info", f.info).Methods("GET")

	return f, nil
}

func (f *faucet) loadAccount(rpc api.BitsharesAPI, name string) *objects.Account {
	dbAPI, err := rpc.DatabaseAPI()
	if err != nil {
		f.log.Fatalf("Failed to get database API: %v", err)
	}
	acc, err := dbAPI.GetAccountByName(name)
	if err != nil {
		f.log.Fatalf("Failed to get account '%s': %v", name, err)
	}
	return acc
}

func (f *faucet) Start(rpc api.BitsharesAPI) {
	f.rpc = rpc

	f.registrar = f.loadAccount(rpc, f.cfg.Registrar)
	f.defaultReferrer = f.loadAccount(rpc, f.cfg.DefaultReferrer)

	go func() {
		if err := f.httpServer.ListenAndServe(); err != nil {
			f.log.Errorf("Unable to start faucet: %v", err)
		}
	}()
}

func (f *faucet) Stop() {
	if err := f.httpServer.Shutdown(context.Background()); err != nil {
		f.log.Errorf("Unable to shutdown server: %v", err)
	}
	os.Exit(69)
}

func (f *faucet) SignalHandler(s os.Signal) {
	f.log.Warnf("Got %s signal...", s.String())
}

func (f *faucet) isAllowedAccountName(w http.ResponseWriter, name string) bool {
	if len(name) < 3 || !objects.IsValidAccountName(name) {
		writeJSONResponse(w, http.StatusBadRequest, NewErrorResponse(ErrInvalidName, "Invalid account name"))
		return false
	}

	if !strings.ContainsAny(name, "-.0123456789") {
		writeJSONResponse(w, http.StatusBadRequest, NewErrorResponse(ErrInvalidName, "Account name must contain digits or hyphen (-) or full stop (.)"))
		return false
	}
	return true
}

func (f *faucet) accounts(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		writeJSONResponse(w, http.StatusBadRequest, NewErrorResponse(ErrInvalidArgument, "Empty request"))
		return
	}

	var req struct {
		Account *accountInfo `json:"account"`
	}

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeJSONResponse(w, http.StatusBadRequest, NewErrorResponse(ErrInvalidArgument, "Failed to parse JSON"))
		return
	}

	if req.Account == nil {
		writeJSONResponse(w, http.StatusBadRequest, NewErrorResponse(ErrInvalidArgument, "Expected account"))
		return
	}

	if !f.isAllowedAccountName(w, req.Account.Name) {
		return
	}

	if f.rateLimiter != nil {
		remoteIP := RemoteIP(r)
		if f.rateLimiter.Get(remoteIP).TakeAvailable(1) == 0 {
			setRateLimitHeaders(w, f.cfg.RateLimiterConfig, remoteIP)
			writeJSONResponse(w, http.StatusTooManyRequests,
				NewErrorResponse(ErrRateLimit, "You have reached maximum request limit."))
			return
		}
	}

	op := objects.AccountCreateOperation{
		Name:            req.Account.Name,
		Registrar:       f.registrar.ID,
		Referrer:        f.defaultReferrer.ID,
		ReferrerPercent: objects.UInt16(f.cfg.ReferrerPercent),
		Owner:           createKeyAuthority(req.Account.OwnerKey),
		Active:          createKeyAuthority(req.Account.ActiveKey),
	}

	memoKey := objects.NewPublicKey(req.Account.MemoKey)
	if memoKey.Valid() {
		op.Options.MemoKey = objects.NewPublicKey(req.Account.MemoKey)
	}

	// set registrar as voting proxy
	op.Options.VotingAccount = f.registrar.ID
	op.Options.Extensions = objects.Extensions{}
	op.Options.Votes = make([]objects.Vote, 0)

	_, err = api.SignAndBroadcast(f.rpc, f.wallet.GetKeys(), objects.NewGrapheneID("1.3.0"), &op)
	if err != nil {
		f.log.Errorw(fmt.Sprintf("Failed to create account: %v", err), "account", req.Account.Name)
		writeJSONResponse(w, http.StatusInternalServerError, NewErrorResponse(ErrOperationFailed, err.Error()))
		return
	}

	f.log.Infow(fmt.Sprintf("Created account: %#v", *req.Account), "account", req.Account.Name)

	writeJSONResponse(w, http.StatusOK, req)
}

func (f *faucet) info(w http.ResponseWriter, r *http.Request) {
	type InfoReply struct {
		Info struct {
			Status    string `json:"status"`
			Registrar string `json:"registrar"`
		} `json:"info"`
	}

	req := &InfoReply{}
	req.Info.Status = "OK"
	req.Info.Registrar = f.registrar.ID.String()
	writeJSONResponse(w, http.StatusOK, req)
}
