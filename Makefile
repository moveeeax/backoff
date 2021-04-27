.PHONY: test vet build

test:
	go test -race ./...

vet:
	go vet ./...

build:
	go build ./...
