package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Merhaba! Go ile yazdığın ilk HTTP servisi çalışıyor.")
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

var productCollection *mongo.Collection

func main() {
	clientOptions := options.Client().ApplyURI("mongodb://mongodb:27017")

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)

	defer cancel()

	client, err := mongo.Connect(clientOptions)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	db := client.Database("test_db")

	productCollection = db.Collection("product")

	router := http.NewServeMux()

	router.HandleFunc("/", helloHandler)

	router.HandleFunc("GET /products/", getProducts)
	router.HandleFunc("GET /products/{id}/", getProductByID)
	router.HandleFunc("POST /products/", createProduct)

	// 3. Sunucuyu Başlatma: 8080 portunu dinlemeye alıyoruz.
	fmt.Println("Sunucu çalışmaya başladı http://localhost:8080 adresinden erişebilirsin.")

	err = http.ListenAndServe(":8080", router)
	if err != nil {
		log.Fatal("Sunucu başlatılamadı", err)
	}
}

type Product struct {
	ID          bson.ObjectID `json:"id" bson:"_id,omitempty"`
	Name        string        `json:"name" bson:"name"`
	Description string        `json:"description" bson:"description"`
	Price       float64       `json:"price" bson:"price"`
	Stock       int           `json:"stock" bson:"stock"`
	Category    string        `json:"category" bson:"category"`
	CreatedAt   time.Time     `json:"createdAt" bson:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt" bson:"updatedAt"`
}

func getProducts(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()

	cursor, err := productCollection.Find(ctx, bson.D{})
	if err != nil {
		WriteJSONResponse(w, http.StatusInternalServerError, map[string]string{"error": "Couldn't find products"})
		return
	}
	defer cursor.Close(ctx)

	var results []Product
	if err := cursor.All(ctx, &results); err != nil {
		WriteJSONResponse(w, http.StatusInternalServerError, map[string]string{"error": "Couldn't convert data"})
		return
	}
	response := map[string]any{
		"message":  "Done",
		"products": results,
	}
	WriteJSONResponse(w, http.StatusOK, response)
}

func getProductByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		WriteJSONResponse(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID format"})
		return
	}
	var foundProduct Product

	err = productCollection.FindOne(ctx, bson.M{"_id": objID}).Decode(&foundProduct)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			WriteJSONResponse(w, http.StatusNotFound, map[string]string{"error": "Product not found"})
			return
		}
		WriteJSONResponse(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}

	response := map[string]any{
		"message": "Done",
		"product": foundProduct,
	}
	WriteJSONResponse(w, http.StatusOK, response)

}

func createProduct(w http.ResponseWriter, r *http.Request) {
	var newProduct Product

	err := ParseJSON(r, &newProduct)
	if err != nil {
		response := map[string]any{
			"message": "Error",
			"product": map[string]any{"message": "Invalid request format"},
		}
		WriteJSONResponse(w, http.StatusBadRequest, response)
		return
	}

	ctx := r.Context()
	newProduct.ID = bson.NewObjectID()
	newProduct.CreatedAt = time.Now()
	newProduct.UpdatedAt = time.Now()

	result, err := productCollection.InsertOne(ctx, newProduct)
	if err != nil {
		response := map[string]any{
			"message": "Error",
			"product": map[string]any{"message": "Failed to save product to database"},
		}
		WriteJSONResponse(w, http.StatusInternalServerError, response)
		return
	}

	log.Printf("Inserted user with ID: %v\n", result.InsertedID)

	response := map[string]any{
		"message": "Done",
		"product": newProduct,
	}
	WriteJSONResponse(w, http.StatusCreated, response)

}
