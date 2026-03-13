REGISTRY ?= freshost
IMAGE ?= $(REGISTRY)/velero-plugin-for-sftp
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: build
build:
	CGO_ENABLED=0 go build -v -o velero-plugin-for-sftp .

.PHONY: test
test:
	go test -v ./...

.PHONY: container
container:
	docker build -t $(IMAGE):$(VERSION) .

.PHONY: push
push: container
	docker push $(IMAGE):$(VERSION)

.PHONY: clean
clean:
	rm -f velero-plugin-for-sftp

.PHONY: lint
lint:
	golangci-lint run ./...
