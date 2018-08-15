package main

import (
	"log"

	"github.com/gin-gonic/gin"
)

func serve() (err error) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	log.Println(getUUID())
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	r.Run() // listen and serve on 0.0.0.0:8080
	return
}
