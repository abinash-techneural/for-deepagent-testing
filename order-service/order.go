package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rabbitmq/amqp091-go"
)

var db *sql.DB
var rabbitChannel *amqp091.Channel
var k keyfunc.Keyfunc

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		head := r.Header.Get("Authorization")

		if head == "" {
			response := map[string]any{
				"message": "Error",
				"order":   map[string]any{"message": "Missing authorization header"},
			}
			WriteJSONResponse(w, http.StatusUnauthorized, response)
			return
		}

		signed := strings.TrimPrefix(head, "Bearer ")
		parsed, err := jwt.Parse(signed, k.Keyfunc)
		if err != nil {
			log.Println("JWT parse failed:", err)
			response := map[string]any{
				"message": "Error",
				"order":   map[string]any{"message": "Invalid or expired token"},
			}

			WriteJSONResponse(w, http.StatusUnauthorized, response)
			return
		}
		log.Println("Token valid for:", parsed)

		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok || !parsed.Valid {
			response := map[string]any{
				"message": "Error",
				"order":   map[string]any{"message": "Invalid token claims"},
			}
			WriteJSONResponse(w, http.StatusUnauthorized, response)
			return
		}

		userID := claims["sub"].(string)
		ctx := context.WithValue(r.Context(), "user_id", userID)
		next(w, r.WithContext(ctx))
	}
}

func main() {

	jwksURL := os.Getenv("KEYCLOAK_JWKS_URL")
	if jwksURL == "" {
		log.Fatal("KEYCLOAK_JWKS_URL environment variable is not set")
	}

	ctx := context.Background()
	var err error
	k, err = keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {

		log.Fatalf("Failed to create a keyfunc.Keyfunc from the server's URL.\nError: %s", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	var err1 error
	db, err1 = sql.Open("pgx", dsn)

	if err1 != nil {
		log.Fatal("Unable to connect to the database", err1)
	}

	err2 := db.Ping()
	if err2 != nil {
		log.Fatal("Unable to connect to the database", err2)

	}

	fmt.Println("Connected to the database")
	url_r := os.Getenv("URL")

	conn, err := amqp091.Dial(url_r)
	if err != nil {
		log.Fatal(err, "Failed to connect to RabbitMQ")
	}
	defer conn.Close()
	rabbitChannel, err1 = conn.Channel()
	if err1 != nil {
		log.Fatal(err1, "Failed to connect to channel")
	}

	defer rabbitChannel.Close()

	err = rabbitChannel.ExchangeDeclare("order_events", "fanout", true, false, false, false, nil)

	if err != nil {
		log.Fatal(err, "Failed to declare exchange")
	}

	_, err3 := db.Exec(`CREATE TABLE IF NOT EXISTS orders (
	id SERIAL PRIMARY KEY,
	customer_id VARCHAR(255),
	status VARCHAR(30),
	total_amount NUMERIC,
	created_at TIMESTAMPTZ,
	updated_at TIMESTAMPTZ
	)`)

	if err3 != nil {
		log.Fatal("Unknown query", err3)
	}

	_, err4 := db.Exec(`CREATE TABLE IF NOT EXISTS order_items (
	id SERIAL PRIMARY KEY,
	order_id INT REFERENCES orders(id),
	product_id VARCHAR(255),
	product_name VARCHAR(255),
	unit_price NUMERIC,
	quantity INT
	)`)

	if err4 != nil {
		log.Fatal("Unknown query", err4)

	}
	router := http.NewServeMux()

	router.HandleFunc("POST /orders/", authMiddleware(createOrder))
	http.ListenAndServe(":8081", router)
}

type CreateOrderRequest struct {
	CustomerID string      `json:"customerId"`
	Items      []OrderItem `json:"items"`
}
type OrderItem struct {
	ProductID string `json:"productId"`
	Quantity  int    `json:"quantity"`
}

type CatalogResponse struct {
	Message string  `json:"message"`
	Product Product `json:"product"`
}

type Product struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

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

func WriteJSONResponse(w http.ResponseWriter, status int, v any) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(v)
}

func getProductFromCatalog(productID string) (Product, error) {
	url := "http://catalog-service:8080/products/" + productID + "/"
	resp, err1 := http.Get(url)

	if err1 != nil {
		return Product{}, fmt.Errorf("Catalog not found %s", productID)

	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return Product{}, fmt.Errorf("Product not found %s", productID)
	}

	var catalogResponse CatalogResponse
	err2 := json.NewDecoder(resp.Body).Decode(&catalogResponse)

	if err2 != nil {
		return Product{}, fmt.Errorf("Couldn't decode %s", productID)
	}
	return catalogResponse.Product, nil
}

func createOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		response := map[string]any{"message": "Error", "order": map[string]any{"message": "User ID not found in context"}}
		WriteJSONResponse(w, http.StatusUnauthorized, response)
		return
	}

	var orderRequest CreateOrderRequest

	err := json.NewDecoder(r.Body).Decode(&orderRequest)

	if err != nil {
		response := map[string]any{
			"message": "Error",
			"order":   map[string]any{"message": "Invalid request format"},
		}
		WriteJSONResponse(w, http.StatusBadRequest, response)
		return
	}

	var validatedItems []Product

	for _, item := range orderRequest.Items {
		res, err := getProductFromCatalog(item.ProductID)
		if err != nil {
			response := map[string]any{
				"message": "Error",
				"order":   map[string]any{"message": fmt.Sprintf("Product not found %s", item.ProductID)},
			}
			WriteJSONResponse(w, http.StatusBadRequest, response)
			return

		}
		validatedItems = append(validatedItems, res)
	}

	var total float64

	for i := range validatedItems {
		total_product := validatedItems[i].Price * float64(orderRequest.Items[i].Quantity)
		total += total_product
	}

	tx, err := db.Begin()

	if err != nil {
		response := map[string]any{
			"message": "Error",
			"order":   map[string]any{"message": fmt.Sprintf("Unable to begin database: %s", err)},
		}
		WriteJSONResponse(w, http.StatusInternalServerError, response)
		return
	}
	var newOrderID int
	err_x := tx.QueryRow(`
INSERT INTO orders (customer_id, status, total_amount, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id
`, userID, "pending", total, time.Now(), time.Now()).Scan(&newOrderID)

	if err_x != nil {
		log.Println("Order insert failed:", err_x)
		response := map[string]any{
			"message": "Error",
			"order":   map[string]any{"message": "Could not create order:"},
		}
		tx.Rollback()
		WriteJSONResponse(w, http.StatusInternalServerError, response)
		return
	}
	for i := range validatedItems {
		_, err_y := tx.Exec(`INSERT INTO order_items (order_id, product_id, product_name, unit_price, quantity)
VALUES ($1, $2, $3, $4, $5)`, newOrderID,
			validatedItems[i].ID,
			validatedItems[i].Name,
			validatedItems[i].Price,
			orderRequest.Items[i].Quantity,
		)

		if err_y != nil {
			log.Println("Order insert failed:", err_y)
			response := map[string]any{
				"message": "Error",
				"order":   map[string]any{"message": "Could not create order:"},
			}
			tx.Rollback()
			WriteJSONResponse(w, http.StatusInternalServerError, response)
			return

		}
	}
	err_c := tx.Commit()

	if err_c != nil {
		log.Println("Order insert failed:", err_c)
		response := map[string]any{
			"message": "Error",
			"order":   map[string]any{"message": "Could not commit order:"},
		}
		WriteJSONResponse(w, http.StatusInternalServerError, response)
		return
	}

	var orderEvent OrderCreatedEvent

	orderEvent.OrderID = newOrderID
	orderEvent.CustomerID = userID
	orderEvent.TotalAmount = total
	orderEvent.CreatedAt = time.Now()

	for l := range validatedItems {
		var orderItemEvent OrderItemEvent
		orderItemEvent.ProductID = validatedItems[l].ID
		orderItemEvent.ProductName = validatedItems[l].Name
		orderItemEvent.UnitPrice = validatedItems[l].Price
		orderItemEvent.Quantity = orderRequest.Items[l].Quantity

		orderEvent.Items = append(orderEvent.Items, orderItemEvent)
	}

	body, err3 := json.Marshal(orderEvent)

	if err3 != nil {
		log.Println("Couldn't marshal order event:", err3)
		response := map[string]any{
			"message": "Done",
			"order": map[string]any{
				"id":     newOrderID,
				"total":  total,
				"status": "pending",
			},
			"warning": "Order created but event could not be published",
		}
		WriteJSONResponse(w, http.StatusCreated, response)
		return
	}

	err4 := rabbitChannel.PublishWithContext(r.Context(), "order_events", "", false, false, amqp091.Publishing{ContentType: "application/json", Body: body})
	if err4 != nil {
		log.Println("Couldn't publish order event:", err4)
		response := map[string]any{
			"message": "Done",
			"order": map[string]any{
				"id":     newOrderID,
				"total":  total,
				"status": "pending",
			},
			"warning": "Order created but event could not be published",
		}
		WriteJSONResponse(w, http.StatusCreated, response)
		return

	}

	response := map[string]any{
		"message": "Done",
		"order":   map[string]any{"id": newOrderID, "total": total, "status": "pending"},
	}
	WriteJSONResponse(w, http.StatusCreated, response)
}
