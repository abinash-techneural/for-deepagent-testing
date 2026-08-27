FROM golang:1.26.4-alpine AS builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
COPY payment.go .


RUN go mod download

RUN go build -o payment-service .


FROM alpine:latest AS runner

WORKDIR /runner_path

COPY --from=builder /app/payment-service .

CMD ["./payment-service"]
