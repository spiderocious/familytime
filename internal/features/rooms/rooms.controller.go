package rooms

import (
	"github.com/gin-gonic/gin"

	"familytime/backend/internal/lib/response"
	"familytime/backend/internal/lib/result"
)

// The controller is thin: read the request, call the service, surface the
// failure or respond.
type controller struct {
	service *Service
}

func (ctl *controller) create(c *gin.Context) {
	var body CreateRoomBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Malformed(c)
		return
	}
	if fieldErrors := body.Validate(); len(fieldErrors) > 0 {
		response.Bail(c, result.Invalid[struct{}](fieldErrors).Failure)
		return
	}

	res := ctl.service.Create(body.GameID, body.Config)
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.Created(c, res.Data)
}

func (ctl *controller) info(c *gin.Context) {
	res := ctl.service.Info(c.Param("code"))
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.OK(c, res.Data)
}

func (ctl *controller) join(c *gin.Context) {
	var body JoinRoomBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Malformed(c)
		return
	}
	if fieldErrors := body.Validate(); len(fieldErrors) > 0 {
		response.Bail(c, result.Invalid[struct{}](fieldErrors).Failure)
		return
	}

	res := ctl.service.Join(c.Param("code"), body.Name, body.Avatar)
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.Created(c, res.Data)
}
