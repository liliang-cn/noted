.PHONY: proto build test run
proto:
	buf lint && buf generate
build:
	CGO_ENABLED=0 go build -trimpath -o bin/noted ./cmd/noted
test:
	go vet ./... && go test -race ./...
run: build
	./bin/noted serve
