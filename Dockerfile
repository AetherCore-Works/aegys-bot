FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/aegys ./cmd/aegys

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 aegys
USER aegys
WORKDIR /app

COPY --from=builder /out/aegys /usr/local/bin/aegys
COPY --chown=aegys:aegys config/commands.example.json /app/config/commands.example.json

EXPOSE 8090
ENTRYPOINT ["aegys"]
