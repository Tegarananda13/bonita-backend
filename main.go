package main

import (
	"bonita-backend/config"
	"bonita-backend/routes"

	"github.com/gin-gonic/gin"
)

func main() {
	config.ConnectDatabase()
	config.Migrate()

	r := gin.Default()

	r.Static("/uploads", "./uploads")

	routes.SetupRoutes(r)

	r.Run(":8080")
}