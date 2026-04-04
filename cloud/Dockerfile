FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
COPY ../pkg ../pkg
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /cloud-api ./cmd/main.go

FROM alpine:3.21
RUN adduser -D -u 1000 loopzero
COPY --from=builder /cloud-api /usr/local/bin/cloud-api
USER loopzero
ENTRYPOINT ["cloud-api"]
