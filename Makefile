# Copyright 2025 Freshost.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

REGISTRY ?= freshost
IMAGE    ?= $(REGISTRY)/velero-plugin-for-sftp
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: build
build:
	CGO_ENABLED=0 go build -v -o velero-plugin-for-sftp .

.PHONY: test
test:
	go test -v -race -coverprofile=coverage.out ./...

.PHONY: ci
ci: verify-modules test

.PHONY: verify-modules
verify-modules:
	go mod verify

.PHONY: container
container:
	docker build -t $(IMAGE):$(VERSION) --build-arg VERSION=$(VERSION) .

.PHONY: push
push: container
	docker push $(IMAGE):$(VERSION)

.PHONY: clean
clean:
	rm -f velero-plugin-for-sftp coverage.out

.PHONY: lint
lint:
	golangci-lint run ./...
