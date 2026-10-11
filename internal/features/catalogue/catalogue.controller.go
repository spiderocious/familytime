package catalogue

import (
	"github.com/gin-gonic/gin"

	"familytime/backend/internal/lib/response"
)

type controller struct {
	service *Service
}

func (ctl *controller) list(c *gin.Context) {
	res := ctl.service.List()
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.OK(c, res.Data)
}

func (ctl *controller) options(c *gin.Context) {
	res := ctl.service.Options(c.Param("gameId"))
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.OK(c, res.Data)
}
