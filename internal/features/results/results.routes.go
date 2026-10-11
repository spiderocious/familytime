package results

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"familytime/backend/internal/lib/response"
)

type controller struct {
	service *Service
}

func (ctl *controller) list(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))

	res, err := ctl.service.List(c.Request.Context(), c.Query("cursor"), limit)
	if err != nil {
		_ = c.Error(err)
		c.Abort()
		return
	}
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.Page(c, res.Data.Items, response.Meta{NextCursor: res.Data.NextCursor, HasMore: res.Data.HasMore})
}

func Register(app *gin.Engine, service *Service) {
	ctl := &controller{service: service}
	app.Group("/api/v1").GET("/results", ctl.list)
}
