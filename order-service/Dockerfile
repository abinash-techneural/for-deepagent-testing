FROM golang:1.26.4-alpine AS builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
COPY order.go .


RUN go mod download

RUN go build -o order-service .


FROM alpine:latest AS runner

WORKDIR /runner_path

COPY --from=builder /app/order-service .

EXPOSE 8081

CMD ["./order-service"]
