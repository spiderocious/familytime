// Package realtime is the websocket gateway. Pure transport: it bridges a
// socket to a room and owns no game logic at all.
package realtime

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/lib/ids"
	"familytime/backend/internal/lib/logger"
	"familytime/backend/internal/lib/response"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

const (
	writeWait = 10 * time.Second
	// Proxies close idle connections at 30–60s; a ping well inside that keeps
	// a quiet lobby alive.
	pingPeriod     = 25 * time.Second
	pongWait       = 60 * time.Second
	maxMessageSize = 8 * 1024
	sendBuffer     = 64

	// Per connection. Generous for a human, tight for a script.
	rateBurst       = 30
	ratePerSecond   = 15
	roleQueryHost   = "host"
	roleQueryPlayer = "player"
)

// Rooms is the part of the engine registry the gateway looks rooms up in.
type Rooms interface {
	Get(code string) (*engine.Room, bool)
}

type inbound struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type client struct {
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
	once sync.Once
}

func (c *client) close() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// push never blocks: it is called with the room's lock held, and one slow
// phone must not stall the game for everyone else. A client that cannot keep
// up is dropped; it reconnects and gets the full state again.
func (c *client) push(envelope engine.Envelope) {
	data, err := json.Marshal(envelope)
	if err != nil {
		logger.Error("could not encode envelope", "type", envelope.Type, "error", err.Error())
		return
	}
	select {
	case <-c.done:
	case c.send <- data:
	default:
		c.close()
	}
}

// Origins is the policy the upgrade checks. The same one CORS uses.
type Origins interface {
	Allows(origin string) bool
}

func Register(app *gin.Engine, rooms Rooms, origins Origins) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			// No Origin header means a non-browser client (a test, a script).
			return origin == "" || origins.Allows(origin)
		},
	}

	app.GET("/ws", func(c *gin.Context) {
		room, ok := rooms.Get(c.Query("code"))
		if !ok {
			response.Bail(c, &result.Failure{Code: constants.CodeRoomNotFound, MessageKey: messages.RoomsNotFound, Status: constants.StatusNotFound})
			return
		}

		role := engine.AudiencePlayer
		switch c.Query("role") {
		case roleQueryHost:
			role = engine.AudienceHost
		case roleQueryPlayer:
		default:
			response.Bail(c, &result.Failure{Code: constants.CodeValidation, MessageKey: messages.CommonValidation, Status: constants.StatusUnprocessable, RejectionReason: "invalid_role"})
			return
		}

		// Authenticated BEFORE the upgrade, so a bad token is an ordinary HTTP
		// error in the standard envelope rather than a socket that opens and dies.
		audience, failure := room.Authenticate(role, c.Query("token"))
		if failure != nil {
			response.Bail(c, failure)
			return
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return // the upgrader has already written the response
		}
		serve(conn, room, audience)
	})
}

func serve(conn *websocket.Conn, room *engine.Room, audience engine.Audience) {
	cl := &client{conn: conn, send: make(chan []byte, sendBuffer), done: make(chan struct{})}
	sub := &engine.Subscriber{ID: ids.New(ids.Conn), Audience: audience, Send: cl.push}

	go writePump(cl)
	room.Attach(sub)

	defer func() {
		room.Detach(sub.ID)
		cl.close()
	}()

	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	tokens := float64(rateBurst)
	last := time.Now()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		now := time.Now()
		tokens = min(rateBurst, tokens+now.Sub(last).Seconds()*ratePerSecond)
		last = now
		if tokens < 1 {
			cl.push(engine.ErrorEnvelope(&result.Failure{Code: constants.CodeRateLimited, MessageKey: messages.CommonRateLimited, Status: constants.StatusTooMany}))
			continue
		}
		tokens--

		var msg inbound
		if err := json.Unmarshal(raw, &msg); err != nil || msg.Type == "" {
			cl.push(engine.ErrorEnvelope(&result.Failure{Code: constants.CodeMalformedJSON, MessageKey: messages.CommonMalformedJSON, Status: constants.StatusBadRequest}))
			continue
		}

		if failure := room.Handle(audience, msg.Type, msg.Payload); failure != nil {
			cl.push(engine.ErrorEnvelope(failure))
		}
	}
}

func writePump(cl *client) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-cl.done:
			return
		case data := <-cl.send:
			_ = cl.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := cl.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				cl.close()
				return
			}
		case <-ticker.C:
			_ = cl.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := cl.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				cl.close()
				return
			}
		}
	}
}
