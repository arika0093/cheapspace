FROM node:20-bookworm-slim AS assets
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install
COPY web ./web
RUN npm run build:css

FROM golang:1.24-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=assets /app/web/static/app.css ./web/static/app.css
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/cheapspace ./cmd/cheapspace

FROM docker:28-cli AS dockercli

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git && rm -rf /var/lib/apt/lists/*
RUN curl -sSL https://nixpacks.com/install.sh | bash
COPY --from=dockercli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=build /out/cheapspace /usr/local/bin/cheapspace
WORKDIR /app
ENV CHEAPSPACE_ADDR=0.0.0.0:8080 \
    CHEAPSPACE_DATA_DIR=/data \
    CHEAPSPACE_AUTO_MIGRATE=true \
    CHEAPSPACE_RUNTIME=docker
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["cheapspace"]
CMD ["serve"]

