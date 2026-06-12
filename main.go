package main

import (
	"log"

	"bonita-backend/config"
	"bonita-backend/routes"

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

	r := gin.Default()

	routes.SetupRoutes(r)

	r.Run(":8080")
}