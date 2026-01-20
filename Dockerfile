# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o manager ./cmd/manager

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache docker-cli

WORKDIR /app
COPY --from=builder /app/manager .

ENTRYPOINT ["/app/manager"]
