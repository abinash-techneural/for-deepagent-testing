FROM golang:1.26.4-alpine AS builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
COPY main.go .


RUN go mod download

RUN go build -o cart-service .


FROM alpine:latest AS runner

WORKDIR /runner_path

COPY --from=builder /app/cart-service .

EXPOSE 8082

CMD ["./cart-service"]
