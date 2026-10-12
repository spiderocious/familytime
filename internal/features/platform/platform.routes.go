package platform

import "github.com/gin-gonic/gin"

func Register(app *gin.Engine, service *Service) {
	ctl := &controller{service: service}
	v1 := app.Group("/api/v1/platform")
	v1.GET("/status", ctl.status)
	v1.POST("/solo", ctl.startSolo)
	v1.POST("/matches/:matchId/join", ctl.joinMatch)
}
