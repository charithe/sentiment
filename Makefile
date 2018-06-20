DOCKER_IMAGE:=charithe/sentiment

.PHONY: build test container docker clean

vendor:
	@dep ensure

test: vendor
	@go test ./...

build: test
	@go build ./cmd/serve.go

docker: 
	@docker build --rm -t $(DOCKER_IMAGE) .

container:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-w -s' ./cmd/serve.go

clean:
	@go clean
	@-rm serve
