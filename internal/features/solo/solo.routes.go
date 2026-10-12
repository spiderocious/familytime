package solo

import "github.com/gin-gonic/gin"

func Register(app *gin.Engine, service *Service) {
	ctl := &controller{service: service}
	app.Group("/api/v1").POST("/solo", ctl.start)
}
