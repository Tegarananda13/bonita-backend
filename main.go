package main

import (
	"log"

	"bonita-backend/config"
	"bonita-backend/routes"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
    "github.com/gin-contrib/cors"
)

func main() {
	err := godotenv.Load()

	if err != nil {
		log.Fatal("Gagal membaca file .env")
	}

	config.ConnectDatabase()
	config.Migrate()

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{
			"http://localhost:5173",
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

	r.Run(":8080")
}