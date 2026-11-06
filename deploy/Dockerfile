# Сборка: docker build -f deploy/Dockerfile -t autoinvest .
FROM node:22-alpine AS web
WORKDIR /src/web/app
COPY web/app/package.json web/app/package-lock.json ./
RUN npm ci
COPY web/app ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY --from=web /src/web/dist ./web/dist
RUN go mod tidy && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/autoinvest ./cmd/autoinvest

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 app
COPY --from=build /out/autoinvest /usr/local/bin/autoinvest
USER app
WORKDIR /var/lib/autoinvest
ENV DATA_DIR=/var/lib/autoinvest APP_ENV=production ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["autoinvest"]
