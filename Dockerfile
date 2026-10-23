FROM golang:1.24-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -buildvcs=false -ldflags="-s -w" -o /out/uav-camera-calibration-scanner ./cmd/uav-camera-calibration-scanner

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/uav-camera-calibration-scanner /usr/local/bin/uav-camera-calibration-scanner
COPY configs/config.yml /app/configs/config.yml
RUN mkdir -p /app/out/docker/artifacts /app/out/docker/results /app/out/docker/run && chown -R app:app /app

USER app
ENV TZ=Asia/Shanghai
ENTRYPOINT ["uav-camera-calibration-scanner"]
CMD ["-config", "/app/configs/config.yml", "-mode", "schedule"]
