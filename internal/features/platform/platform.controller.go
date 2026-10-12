package platform

import (
	"io"
	"net/http"
	"strings"

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

func (ctl *controller) wallet(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	res := ctl.service.Wallet(c.Request.Context(), body.Session)
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.OK(c, res.Data)
}

func (ctl *controller) claimPurchase(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	res := ctl.service.ClaimPurchase(c.Request.Context(), body.Session, c.Param("purchaseId"))
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.OK(c, res.Data)
}

func (ctl *controller) reveal(c *gin.Context) {
	body, ok := bind(c)
	if !ok {
		return
	}
	res := ctl.service.Reveal(c.Request.Context(), body.Session, strings.ToUpper(c.Param("code")), body.QuestionSeq)
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.OK(c, res.Data)
}

// webhook reads the body raw: the signature is over the exact bytes sent.
func (ctl *controller) webhook(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBytes))
	if err != nil {
		response.Malformed(c)
		return
	}
	res := ctl.service.Webhook(c.Request.Context(), raw, c.GetHeader(webhookSignatureHeader))
	if !res.OK() {
		response.Bail(c, res.Failure)
		return
	}
	response.NoContent(c)
}
