package rooms

import "github.com/gin-gonic/gin"

func Register(app *gin.Engine, service *Service) {
	ctl := &controller{service: service}
	v1 := app.Group("/api/v1")

	// The literal path first. There is no literal under /rooms/:code today, but
	// if one is added (/rooms/recent, say) it must sit above the parameter.
	v1.POST("/rooms", ctl.create)
	v1.GET("/rooms/:code", ctl.info)
	v1.POST("/rooms/:code/players", ctl.join)
}
