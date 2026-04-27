FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev git

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /ts-symbols .

FROM alpine:3.21
COPY --from=builder /ts-symbols /usr/local/bin/ts-symbols
ENTRYPOINT ["/usr/local/bin/ts-symbols"]
