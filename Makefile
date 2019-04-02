.PHONY: build faucet

build: faucet

faucet:
	go build -o bin/otn-faucet .

install-deps:
	dep ensure -v

update-deps:
	dep ensure -update -v
