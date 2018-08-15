package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var wsupgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func serve() (err error) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	log.Println(getUUID())
	r.LoadHTMLGlob("templates/*")
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "Main website",
		})
	})
	r.GET("/ws", func(cg *gin.Context) {
		c, err := wsupgrader.Upgrade(cg.Writer, cg.Request, nil)
		if err != nil {
			log.Print("upgrade:", err)
			return
		}
		defer c.Close()
		var p Payload
		for {
			err := c.ReadJSON(&p)
			if err != nil {
				log.Println("read:", err)
				break
			}
			log.Printf("recv: %v", p)
			err = c.WriteJSON(Payload{
				Message: "got it",
				Success: true,
			})
			if err != nil {
				log.Println("write:", err)
				break
			}
		}
	})
	log.Printf("running on port 8080")
	r.Run() // listen and serve on 0.0.0.0:8080
	return
}
