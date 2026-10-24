package main

import (
	"log"

	"bonita-backend/config"
	"bonita-backend/controllers/admin"
	"bonita-backend/routes"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()

	if err != nil {
		log.Fatal("Gagal membaca file .env")
	}

	config.ConnectDatabase()
	config.Migrate()
	config.Seed()

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{
			"http://localhost:5173",
			// "https://bonita-frontend-iota.vercel.app",
			// "http://192.168.110.174:5173",
		},
		AllowMethods: []string{
			"GET",
			"POST",
			"PUT",
			"PATCH",
			"DELETE",
			"OPTIONS",
		},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
		},
		AllowCredentials: true,
	}))

	routes.SetupRoutes(r)

	// Mulai scheduler expiry DP di background
	admin.StartExpiryScheduler()

	r.Run(":8080")
	// r.Run("0.0.0.0:8080")
}
