BIN := bin/townsquare

.PHONY: build run test vet fmt clean docker

build:
	go build -trimpath -o $(BIN) ./cmd/townsquare

run:
	go run ./cmd/townsquare

test:
	go test ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -rf bin

docker:
	docker build -t townsquare:latest .
