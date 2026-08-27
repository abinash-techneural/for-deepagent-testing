FROM golang:1.26.4-alpine AS builder

WORKDIR /app

COPY . .
RUN go build -o catalog-service .

FROM alpine:latest AS runner 

WORKDIR /runner_path

COPY --from=builder /app/catalog-service .

EXPOSE 8080

CMD ["./catalog-service"]
