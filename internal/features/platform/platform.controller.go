package platform

import (
	"github.com/gin-gonic/gin"

	"familytime/backend/internal/lib/response"
	"familytime/backend/internal/lib/result"
)

type controller struct {
	service *Service
}

func (ctl *controller) status(c *gin.Context) {
	res := ctl.service.Status()
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.OK(c, res.Data)
}

// bind reads and validates the session body, or reports why it could not.
func bind(c *gin.Context) (SessionBody, bool) {
	var body SessionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Malformed(c)
		return body, false
	}
	if fieldErrors := body.Validate(); len(fieldErrors) > 0 {
		response.Bail(c, result.Invalid[struct{}](fieldErrors).Failure)
		return body, false
	}
	return body, true
}

func (ctl *controller) startSolo(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	res := ctl.service.StartSolo(c.Request.Context(), body.Session, body.Config)
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.Created(c, res.Data)
}

func (ctl *controller) joinMatch(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	res := ctl.service.JoinMatch(c.Request.Context(), c.Param("matchId"), body.Session)
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.OK(c, res.Data)
}
