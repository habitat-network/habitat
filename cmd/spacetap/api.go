package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type APIServer struct {
	db       *gorm.DB
	syncer   *Syncer
	hub      *WebSocketHub
	cfg      *Config
	log      zerolog.Logger
	started  time.Time
	upgrader websocket.Upgrader
}

func NewAPIServer(db *gorm.DB, syncer *Syncer, hub *WebSocketHub, cfg *Config, log zerolog.Logger) *APIServer {
	return &APIServer{
		db:      db,
		syncer:  syncer,
		hub:     hub,
		cfg:     cfg,
		log:     log,
		started: time.Now(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (a *APIServer) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/health", a.HandleHealth).Methods("GET")
	r.HandleFunc("/channel", a.HandleChannel).Methods("GET")
	r.HandleFunc("/stats", a.HandleStats).Methods("GET")
	r.HandleFunc("/info/space/{space}", a.HandleSpaceInfo).Methods("GET")
	r.HandleFunc("/spaces/resync", a.HandleResync).Methods("POST")
}

func (a *APIServer) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
	})
}

func (a *APIServer) HandleChannel(w http.ResponseWriter, r *http.Request) {
	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		a.log.Warn().Err(err).Msg("channel websocket upgrade failed")
		return
	}

	client := &WSClient{conn: conn, done: make(chan struct{})}
	a.hub.Add(client)
	a.log.Info().Msg("app consumer connected")

	defer func() {
		a.hub.Remove(client)
		conn.Close()
		close(client.done)
		a.log.Info().Msg("app consumer disconnected")
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var ack struct {
			Ack uint `json:"ack"`
		}
		if json.Unmarshal(message, &ack) == nil && ack.Ack > 0 {
			a.db.Model(&OutboxEvent{}).Where("id = ?", ack.Ack).Update("acked", true)
			a.log.Debug().Uint("event_id", ack.Ack).Msg("ack received")
		}
	}
}

func (a *APIServer) HandleStats(w http.ResponseWriter, r *http.Request) {
	var spaceCount int64
	var repoCount int64
	var recordCount int64
	var memberCount int64
	var outboxCount int64

	a.db.Model(&SpaceState{}).Count(&spaceCount)
	a.db.Model(&RepoState{}).Count(&repoCount)
	a.db.Model(&RecordState{}).Count(&recordCount)
	a.db.Model(&MemberState{}).Count(&memberCount)
	a.db.Model(&OutboxEvent{}).Where("acked = ?", false).Count(&outboxCount)

	deliveryMode := "websocket"
	if a.cfg.WebhookURL != "" {
		deliveryMode = "webhook"
	}
	if a.cfg.DisableAcks {
		deliveryMode = "fire-and-forget"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"spaces":        spaceCount,
		"repos":         repoCount,
		"records":       recordCount,
		"members":       memberCount,
		"outbox_depth":  outboxCount,
		"delivery_mode": deliveryMode,
		"uptime":        time.Since(a.started).String(),
	})
}

func (a *APIServer) HandleSpaceInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	space := vars["space"]

	var state SpaceState
	if err := a.db.Where("space = ?", space).First(&state).Error; err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": "space not found",
		})
		return
	}

	var repoCount int64
	var recordCount int64
	a.db.Model(&RepoState{}).Where("space = ?", space).Count(&repoCount)
	a.db.Model(&RecordState{}).Where("space = ?", space).Count(&recordCount)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"space":       state.Space,
		"space_type":  state.SpaceType,
		"state":       state.State,
		"space_rev":   state.SpaceRev,
		"member_rev":  state.MemberRev,
		"repos":       repoCount,
		"records":     recordCount,
	})
}

func (a *APIServer) HandleResync(w http.ResponseWriter, r *http.Request) {
	go func() {
		if err := a.syncer.Run(context.Background()); err != nil {
			a.log.Warn().Err(err).Msg("resync failed")
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status": "resync initiated",
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
