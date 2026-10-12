package solo

import (
	"github.com/gin-gonic/gin"

	"familytime/backend/internal/lib/response"
	"familytime/backend/internal/lib/result"
)

type controller struct {
	service *Service
}

func (ctl *controller) start(c *gin.Context) {
	var body StartBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Malformed(c)
		return
	}
	if fieldErrors := body.Validate(); len(fieldErrors) > 0 {
		response.Bail(c, result.Invalid[struct{}](fieldErrors).Failure)
		return
	}

	res := ctl.service.Start(body.Name, body.Avatar, body.Config)
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.Created(c, res.Data)
}
