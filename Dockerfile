FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOPROXY=https://proxy.golang.org

WORKDIR /go/src/velero-plugin-for-sftp
COPY . .
RUN CGO_ENABLED=0 go build -v -o /go/bin/velero-plugin-for-sftp .

FROM busybox:1.37
COPY --from=build /go/bin/velero-plugin-for-sftp /plugins/
USER 65532:65532
ENTRYPOINT ["cp", "/plugins/velero-plugin-for-sftp", "/target/."]
