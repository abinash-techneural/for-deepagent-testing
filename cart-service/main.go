package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

type Product struct {
	ProductID string `json:"id"`
	Quantity  int    `json:"quantity"`
}

func WriteJSONResponse(w http.ResponseWriter, status int, v any) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(v)
}

func ParseJSON(r *http.Request, payload any) error {
	if r.Body == nil {
		return fmt.Errorf("missing body request")
	}

	return json.NewDecoder(r.Body).Decode(payload)
}

var redisClient *redis.Client
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

	redisClient = redis.NewClient(&redis.Options{Addr: "redis:6379"})

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatal("Unable to connect to Redis", err)
	}

	defer redisClient.Close()

	router := http.NewServeMux()

	router.HandleFunc("GET /cart/", authMiddleware(getCart))
	router.HandleFunc("POST /cart/", authMiddleware(addProductCart))
	router.HandleFunc("DELETE /cart/", authMiddleware(removeProductFromCart))

	err = http.ListenAndServe(":8082", router)
	if err != nil {
		log.Fatal("Unable to connext server", err)
	}

}

func addProductCart(w http.ResponseWriter, r *http.Request) {

	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		response := map[string]any{"message": "Error", "cart": map[string]any{"message": "User ID not found"}}
		WriteJSONResponse(w, http.StatusUnauthorized, response)
		return
	}

	var newProduct Product

	err1 := ParseJSON(r, &newProduct)

	if err1 != nil {
		response := map[string]any{
			"message": "Error",
			"product": map[string]any{"message": "Invalid request format"},
		}
		WriteJSONResponse(w, http.StatusBadRequest, response)
		return
	}

	id := newProduct.ProductID
	quantity := newProduct.Quantity

	ctx := r.Context()

	_, err2 := redisClient.HSet(ctx, "cart:"+userID, id, quantity).Result()

	if err2 != nil {
		response := map[string]any{
			"message": "Error",
			"product": map[string]any{"message": "Failed to save product to cart"},
		}
		WriteJSONResponse(w, http.StatusInternalServerError, response)
		return
	}
	err1 = redisClient.Expire(ctx, "cart:"+userID, 30*time.Minute).Err()

	if err1 != nil {
		log.Println("Warning: Failed to set TTL for cart:", err1)
	}

	response := map[string]any{
		"message": "Done",
		"product": newProduct,
	}
	WriteJSONResponse(w, http.StatusCreated, response)
}

func getCart(w http.ResponseWriter, r *http.Request) {

	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		response := map[string]any{"message": "Error", "cart": map[string]any{"message": "User ID not found"}}
		WriteJSONResponse(w, http.StatusUnauthorized, response)
		return
	}

	ctx := r.Context()
	cartData, err1 := redisClient.HGetAll(ctx, "cart:"+userID).Result()

	if err1 != nil {
		response := map[string]any{
			"message": "Error",
			"product": map[string]any{"message": "Failed to show cart"},
		}
		WriteJSONResponse(w, http.StatusInternalServerError, response)
		return
	}

	response := map[string]any{
		"message": "Done",
		"product": cartData,
	}
	WriteJSONResponse(w, http.StatusOK, response)

}

func removeProductFromCart(w http.ResponseWriter, r *http.Request) {

	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		response := map[string]any{"message": "Error", "cart": map[string]any{"message": "User ID not found"}}
		WriteJSONResponse(w, http.StatusUnauthorized, response)
		return
	}

	var newProduct Product

	err1 := ParseJSON(r, &newProduct)

	if err1 != nil {
		response := map[string]any{
			"message": "Error",
			"product": map[string]any{"message": "Invalid request format"},
		}
		WriteJSONResponse(w, http.StatusBadRequest, response)
		return
	}

	ctx := r.Context()

	product_id := newProduct.ProductID

	_, err2 := redisClient.HDel(ctx, "cart:"+userID, product_id).Result()
	if err2 != nil {
		response := map[string]any{
			"message": "Error",
			"product": map[string]any{"message": "Failed to delete product from cart"},
		}
		WriteJSONResponse(w, http.StatusInternalServerError, response)
		return
	}

	response := map[string]any{
		"message": "Done",
		"product": map[string]any{"message": fmt.Sprintf("Product successfully deleted %s", product_id)},
	}
	WriteJSONResponse(w, http.StatusOK, response)

}
