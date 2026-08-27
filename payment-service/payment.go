package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type OrderCreatedEvent struct {
	OrderID     int              `json:"order_id"`
	CustomerID  string           `json:"customer_id"`
	TotalAmount float64          `json:"total_amount"`
	Items       []OrderItemEvent `json:"items"`
	CreatedAt   time.Time        `json:"created_at"`
}

type OrderItemEvent struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	UnitPrice   float64 `json:"unit_price"`
	Quantity    int     `json:"quantity"`
}

var rabbitChannel *amqp091.Channel

func main() {

	url_r := os.Getenv("URL")

	conn, err := amqp091.Dial(url_r)
	if err != nil {
		log.Fatal(err, "Failed to connect to RabbitMQ")
	}
	defer conn.Close()
	rabbitChannel, err = conn.Channel()
	if err != nil {
		log.Fatal(err, "Failed to connect to channel")
	}

	defer rabbitChannel.Close()

	err = rabbitChannel.ExchangeDeclare("order_events", "fanout", true, false, false, false, nil)

	if err != nil {
		log.Fatal(err, "Failed to declare exchange")
	}

	queue, err1 := rabbitChannel.QueueDeclare("payment_queue", true, false, false, false, nil)

	if err1 != nil {
		log.Fatal(err1, "Failed to declare exchange")
	}

	err1 = rabbitChannel.QueueBind("payment_queue", "", "order_events", false, nil)
	if err1 != nil {
		log.Fatal(err1, "Failed to declare exchange")
	}

	chann, err2 := rabbitChannel.Consume(queue.Name, "", false, false, false, false, nil)
	if err2 != nil {
		log.Fatal(err2, "Failed to declare exchange")
	}

	for msg := range chann {
		var orderCreatedEvent OrderCreatedEvent
		err2 = json.Unmarshal(msg.Body, &orderCreatedEvent)

		if err2 != nil {
			log.Println("Failed to decode message:", err2)
			continue
		}
		log.Println("Payment processing for order:", orderCreatedEvent.OrderID, "amount:", orderCreatedEvent.TotalAmount)

		msg.Ack(false)

	}

}
