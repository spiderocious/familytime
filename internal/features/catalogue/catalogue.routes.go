package catalogue

import (
	"github.com/gin-gonic/gin"
)

func Register(app *gin.Engine, service *Service) {
	ctl := &controller{service: service}
	v1 := app.Group("/api/v1")
	v1.GET("/games", ctl.list)
	v1.GET("/games/:gameId/options", ctl.options)
}
