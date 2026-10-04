// Package server runs the game in real time and exposes it over HTTP and
// WebSocket. See docs/API.md for the contract.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coder/websocket"

	"myjev/internal/game"
)

const (
	maxQueuedActions = 4
	humanController  = "human"
	maxNameLength    = 32
	maxStatusLength  = 80
)

// conflictError is a request that is well formed but not allowed right
// now, such as steering while someone else is driving.
type conflictError string

func (e conflictError) Error() string { return string(e) }

// State is the game state plus what only the server knows.
type State struct {
	game.State
	RunID      int    `json:"run_id"`
	Controller string `json:"controller"`
	// ControllerStatus is a line of free text from whoever is driving,
	// shown under the driver's name. Empty if they have set none.
	ControllerStatus string `json:"controller_status"`
	BestScore        int    `json:"best_score"`
	// Pace is how fast the game's clock runs against the wall clock:
	// 1 is real time, 0.25 makes every tick last four times as long.
	Pace float64 `json:"pace"`
}

type client struct {
	send chan []byte
	name string // who this connection steers as; "human" until it claims control
}

type Server struct {
	cfg  game.Config
	web  fs.FS
	pace float64 // set before Run, never changed after

	mu         sync.Mutex
	g          *game.Game
	runID      int
	best       int
	controller string // the only name whose input is accepted
	status     string // the controller's status text
	queue      []game.Action
	clients    map[*client]struct{}
}

// New creates a server with a first run already started. web holds the
// viewer's static files.
func New(cfg game.Config, seed uint64, web fs.FS) *Server {
	return &Server{
		cfg:        cfg,
		web:        web,
		pace:       1,
		g:          game.New(cfg, seed),
		runID:      1,
		controller: humanController,
		clients:    map[*client]struct{}{},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /state", s.handleState)
	mux.HandleFunc("POST /input", s.handleInput)
	mux.HandleFunc("POST /reset", s.handleReset)
	mux.HandleFunc("POST /control/claim", s.handleClaim)
	mux.HandleFunc("POST /control/release", s.handleRelease)
	mux.HandleFunc("POST /control/status", s.handleStatus)
	mux.HandleFunc("GET /ws", s.handleWS)
	mux.Handle("GET /", http.FileServerFS(s.web))
	return mux
}

// SetPace slows the game's clock: at pace 0.25 a tick lasts four times
// as long. The game itself is the same at every pace, tick for tick.
// Call it before Run.
func (s *Server) SetPace(pace float64) error {
	if !(pace > 0 && pace <= 1) {
		return fmt.Errorf("pace must be above 0 and at most 1, got %v", pace)
	}
	s.pace = pace
	return nil
}

// tickInterval is how long one tick lasts on the wall clock.
func tickInterval(tickRate int, pace float64) time.Duration {
	return time.Duration(float64(time.Second) / (float64(tickRate) * pace))
}

// Run drives the game at the configured tick rate and pace until ctx is
// done.
func (s *Server) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval(s.cfg.TickRate, s.pace))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *Server) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	action := game.None
	if len(s.queue) > 0 {
		action, s.queue = s.queue[0], s.queue[1:]
	}
	s.g.Step(action)
	// Track the best score here, not only when someone asks for the
	// state, so a run nobody watched still counts.
	s.best = max(s.best, s.g.Score())
	s.broadcastLocked()
}

func (s *Server) stateLocked() State {
	st := State{
		State:      s.g.Snapshot(),
		RunID:      s.runID,
		Controller: s.controller,
		BestScore:  s.best,
		Pace:       s.pace,

		ControllerStatus: s.status,
	}
	return st
}

func (s *Server) broadcastLocked() {
	if len(s.clients) == 0 {
		return
	}
	msg := stateMessage(s.stateLocked())
	for c := range s.clients {
		// A slow client gets the newest state; older ones are dropped.
		select {
		case c.send <- msg:
		default:
			select {
			case <-c.send:
			default:
			}
			select {
			case c.send <- msg:
			default:
			}
		}
	}
}

func stateMessage(st State) []byte {
	msg, _ := json.Marshal(struct {
		Type  string `json:"type"`
		State State  `json:"state"`
	}{"state", st})
	return msg
}

func (s *Server) state() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func (s *Server) input(name, action string) error {
	a, ok := game.ParseAction(action)
	if !ok {
		return errors.New(`action must be "left", "right" or "none"`)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if name != s.controller {
		return conflictError(fmt.Sprintf("%q is not driving; the controller is %q", name, s.controller))
	}
	if a != game.None && !s.g.GameOver() && len(s.queue) < maxQueuedActions {
		s.queue = append(s.queue, a)
	}
	return nil
}

func (s *Server) reset(seed *uint64) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	sd := uint64(rand.Uint32()) // small enough to survive a round trip through JSON numbers
	if seed != nil {
		sd = *seed
	}
	s.g = game.New(s.cfg, sd)
	s.runID++
	s.queue = nil
	s.broadcastLocked()
	return s.stateLocked()
}

// claim makes name the controller. "human" can always take the car
// back; anyone else only gets it from the human.
func (s *Server) claim(name string) error {
	if !validName(name) {
		return fmt.Errorf("name must be 1 to %d printable characters without spaces", maxNameLength)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if name != humanController && s.controller != humanController && s.controller != name {
		return conflictError(fmt.Sprintf("%q is already driving", s.controller))
	}
	s.setControllerLocked(name)
	return nil
}

// release hands the car back to the human. Only the current controller
// can release.
func (s *Server) release(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name != s.controller {
		return conflictError(fmt.Sprintf("%q is not driving; the controller is %q", name, s.controller))
	}
	s.setControllerLocked(humanController)
	return nil
}

func (s *Server) setControllerLocked(name string) {
	if s.controller == name {
		return
	}
	s.controller = name
	s.queue = nil // the previous driver's pending moves do not carry over
	s.status = "" // nor does what they said about themselves
	s.broadcastLocked()
}

// setStatus sets the text shown under the driver's name. Only the
// current controller can set it; an empty text clears it. The next
// state carries it, so nothing is broadcast here.
func (s *Server) setStatus(name, text string) error {
	if !validStatus(text) {
		return fmt.Errorf("status must be at most %d printable characters", maxStatusLength)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if name != s.controller {
		return conflictError(fmt.Sprintf("%q is not driving; the controller is %q", name, s.controller))
	}
	s.status = text
	return nil
}

func validStatus(text string) bool {
	if utf8.RuneCountInString(text) > maxStatusLength {
		return false
	}
	for _, r := range text {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return utf8.ValidString(text)
}

func validName(name string) bool {
	if name == "" || len(name) > maxNameLength {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func (s *Server) controllerName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controller
}

// --- HTTP ---

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.state())
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if !readRequest(w, r, &req) {
		return
	}
	if req.Name == "" {
		req.Name = humanController
	}
	if err := s.input(req.Name, req.Action); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readRequest(w, r, &req) {
		return
	}
	if err := s.claim(req.Name); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "controller": s.controllerName()})
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !readRequest(w, r, &req) {
		return
	}
	if err := s.release(req.Name); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "controller": s.controllerName()})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
		Name string `json:"name"`
	}
	if !readRequest(w, r, &req) {
		return
	}
	if req.Name == "" {
		req.Name = humanController
	}
	if err := s.setStatus(req.Name, req.Text); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seed *uint64 `json:"seed"`
	}
	if !readRequest(w, r, &req) {
		return
	}
	writeJSON(w, http.StatusOK, s.reset(req.Seed))
}

// readRequest checks the origin and decodes the body. On failure it
// writes the error response and reports false.
func readRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-origin request")
		return false
	}
	if err := decodeBody(r, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

// writeFailure maps an error to 409 (not allowed right now) or 400.
func writeFailure(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if _, ok := err.(conflictError); ok {
		status = http.StatusConflict
	}
	writeError(w, status, err.Error())
}

// sameOrigin rejects browser requests coming from another site. Programs
// such as curl or a bot send no Origin header and pass.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

// decodeBody reads a JSON body; an empty body is allowed.
func decodeBody(r *http.Request, v any) error {
	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4096)).Decode(v)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

// --- WebSocket ---

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Accept rejects browser connections from other origins by default.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4096)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	c := &client{send: make(chan []byte, 1), name: humanController}
	s.mu.Lock()
	c.send <- stateMessage(s.stateLocked())
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		// A bot that drops its connection gives the car back.
		if c.name != humanController && s.controller == c.name {
			s.setControllerLocked(humanController)
		}
		s.mu.Unlock()
	}()

	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-c.send:
				if writeWS(ctx, conn, msg) != nil {
					return
				}
			}
		}
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if err := s.handleMessage(c, data); err != nil {
			msg, _ := json.Marshal(map[string]string{"type": "error", "error": err.Error()})
			if writeWS(ctx, conn, msg) != nil {
				return
			}
		}
	}
}

// handleMessage runs on the connection's read loop, the only place that
// touches c.name apart from the disconnect cleanup after it.
func (s *Server) handleMessage(c *client, data []byte) error {
	var m struct {
		Type   string  `json:"type"`
		Action string  `json:"action"`
		Name   string  `json:"name"`
		Text   string  `json:"text"`
		Seed   *uint64 `json:"seed"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.New("invalid JSON message")
	}
	switch m.Type {
	case "input":
		return s.input(c.name, m.Action)
	case "reset":
		s.reset(m.Seed)
		return nil
	case "claim":
		if err := s.claim(m.Name); err != nil {
			return err
		}
		c.name = m.Name
		return nil
	case "status":
		return s.setStatus(c.name, m.Text)
	case "release":
		name := c.name
		c.name = humanController
		return s.release(name)
	}
	return errors.New(`unknown message type "` + m.Type + `"`)
}

func writeWS(ctx context.Context, conn *websocket.Conn, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, msg)
}
