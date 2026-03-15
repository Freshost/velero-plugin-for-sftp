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

FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ENV GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOPROXY=https://proxy.golang.org

WORKDIR /go/src/velero-plugin-for-sftp
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -v -ldflags="-X main.version=${VERSION}" -o /go/bin/velero-plugin-for-sftp .

FROM busybox:1.37

LABEL org.opencontainers.image.title="Velero Plugin for SFTP" \
      org.opencontainers.image.description="A Velero ObjectStore plugin for SFTP storage with optional age encryption" \
      org.opencontainers.image.url="https://github.com/Freshost/velero-plugin-for-sftp" \
      org.opencontainers.image.source="https://github.com/Freshost/velero-plugin-for-sftp" \
      org.opencontainers.image.vendor="Freshost" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=build /go/bin/velero-plugin-for-sftp /plugins/
USER 65532:65532
ENTRYPOINT ["cp", "/plugins/velero-plugin-for-sftp", "/target/."]
