package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/gorilla/mux"
	"github.com/habitat-network/habitat/internal/authn"
	"google.golang.org/api/calendar/v3"
	"gorm.io/gorm"
)

type Server struct {
	router     *mux.Router
	google     *GoogleClient
	store      *Store
	authMethod authn.Method
	domain     string
}

func NewServer(
	googleClient *GoogleClient,
	store *Store,
	authMethod authn.Method,
	domain string,
	debug bool,
) *Server {
	s := &Server{
		router:     mux.NewRouter(),
		google:     googleClient,
		store:      store,
		authMethod: authMethod,
		domain:     domain,
	}

	if debug {
		s.router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				dump, err := httputil.DumpRequest(r, true)
				if err == nil {
					log.Println(string(dump))
				} else {
					log.Println("failed to dump request", err)
				}
				next.ServeHTTP(w, r)
			})
		})
	}

	s.setupRoutes()
	return s
}

func (s *Server) Router() *mux.Router {
	return s.router
}

func (s *Server) setupRoutes() {
	s.router.HandleFunc("/xrpc/network.habitat.calendar.connectGoogle", s.handleConnectGoogle).
		Methods("POST")
	s.router.HandleFunc("/google/callback", s.handleGoogleCallback).Methods("GET")

	s.router.HandleFunc("/xrpc/network.habitat.calendar.getEvents", s.handleGetEvents).
		Methods("GET")
	s.router.HandleFunc("/xrpc/network.habitat.calendar.getEvent", s.handleGetEvent).
		Methods("GET")

	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")

	s.router.HandleFunc("/.well-known/did.json", s.serveDid).Methods("GET")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) serveDid(w http.ResponseWriter, r *http.Request) {
	template := `{
  "id": "did:web:%s",
  "@context": [
    "https://www.w3.org/ns/did/v1",
    "https://w3id.org/security/multikey/v1", 
    "https://w3id.org/security/suites/secp256k1-2019/v1"
  ],
  "service": [
    {
      "id": "#calendar",
      "serviceEndpoint": "https://%s",
      "type": "CalendarServer"
    }
  ]
}`
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, template, s.domain, s.domain)
}

func (s *Server) handleConnectGoogle(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.authMethod.Validate(w, r)
	if !ok {
		return
	}

	_, err := s.store.GetSessionByDID(credInfo.Subject.String())
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("Error getting session by DID: %v", err)
		writeError(
			w,
			"InternalServerError",
			"failed to get session",
			http.StatusInternalServerError,
		)
		return
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		_, err = s.store.CreateSession(credInfo.Subject.String())
		if err != nil {
			log.Printf("Error creating session: %v", err)
			writeError(
				w,
				"InternalServerError",
				"failed to create session",
				http.StatusInternalServerError,
			)
			return
		}
	}

	authURL := s.google.AuthCodeURL(credInfo.Subject.String())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		AuthURL string `json:"authUrl"`
	}{
		AuthURL: authURL,
	})
}

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	did := r.URL.Query().Get("state")
	if did == "" {
		http.Error(w, "missing state parameter", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	tok, _, err := s.google.Exchange(r.Context(), code)
	if err != nil {
		log.Printf("Error exchanging code: %v", err)
		http.Error(w, "failed to exchange code", http.StatusInternalServerError)
		return
	}

	session, err := s.store.GetSessionByDID(did)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		log.Printf("Error getting session: %v", err)
		http.Error(w, "failed to get session", http.StatusInternalServerError)
		return
	}

	err = s.store.UpdateTokens(
		session.ID,
		tok.AccessToken,
		tok.RefreshToken,
		tok.Expiry.Unix(),
	)
	if err != nil {
		log.Printf("Error updating tokens: %v", err)
		http.Error(w, "failed to store tokens", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Google account connected successfully",
	})
}

func (s *Server) getSessionByAuth(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	credInfo, ok := s.authMethod.Validate(w, r)
	if !ok {
		return nil, false
	}

	session, err := s.store.GetSessionByDID(credInfo.Subject.String())
	if err != nil {
		return nil, false
	}
	return session, true
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	dump, _ := httputil.DumpRequest(r, true)
	log.Printf("handle ConnectGoogle Request: %s", dump)
	session, ok := s.getSessionByAuth(w, r)
	if !ok {

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Events []CalendarEvent `json:"events"`
		}{
			Events: []CalendarEvent{},
		})
		return
	}

	if session.GoogleAccessToken == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Events []CalendarEvent `json:"events"`
		}{
			Events: []CalendarEvent{},
		})
		return
	}

	calendarID := r.URL.Query().Get("calendarId")
	if calendarID == "" {
		calendarID = "primary"
	}
	timeMin := r.URL.Query().Get("timeMin")
	timeMax := r.URL.Query().Get("timeMax")

	events, err := s.google.ListEvents(r.Context(), session, calendarID, timeMin, timeMax, 100)
	if err != nil {
		log.Printf("Error listing events: %v", err)
		writeError(
			w,
			"InternalServerError",
			"failed to fetch events",
			http.StatusInternalServerError,
		)
		return
	}

	calEvents := make([]CalendarEvent, 0, len(events))
	for _, e := range events {
		calEvents = append(calEvents, googleEventToCalendarEvent(e))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Events []CalendarEvent `json:"events"`
	}{
		Events: calEvents,
	})
}

func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	session, ok := s.getSessionByAuth(w, r)
	if !ok {
		return
	}

	if session.GoogleAccessToken == "" {
		writeError(w, "Unauthorized", "Google account not connected", http.StatusUnauthorized)
		return
	}

	calendarID := r.URL.Query().Get("calendarId")
	if calendarID == "" {
		calendarID = "primary"
	}
	eventID := r.URL.Query().Get("eventId")
	if eventID == "" {
		writeError(w, "InvalidRequest", "eventId is required", http.StatusBadRequest)
		return
	}

	event, err := s.google.GetEvent(r.Context(), session, calendarID, eventID)
	if err != nil {
		log.Printf("Error getting event: %v", err)
		if strings.Contains(err.Error(), "not found") {
			writeError(w, "NotFound", "event not found", http.StatusNotFound)
			return
		}
		writeError(
			w,
			"InternalServerError",
			"failed to fetch event",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Event CalendarEvent `json:"event"`
	}{
		Event: googleEventToCalendarEvent(event),
	})
}

type ErrorOutput struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func writeError(w http.ResponseWriter, error string, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorOutput{
		Error:   error,
		Message: message,
	})
}

type CalendarEvent struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	CreatedAt   string                  `json:"createdAt"`
	StartsAt    string                  `json:"startsAt,omitempty"`
	EndsAt      string                  `json:"endsAt,omitempty"`
	Mode        string                  `json:"mode,omitempty"`
	Status      string                  `json:"status"`
	Locations   []CalendarEventLocation `json:"locations,omitempty"`
	URIs        []CalendarEventURI      `json:"uris,omitempty"`
}

type CalendarEventLocation struct {
	Type string      `json:"type"`
	URI  interface{} `json:"uri,omitempty"`
}

type CalendarEventURI struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

func googleEventToCalendarEvent(e *calendar.Event) CalendarEvent {
	status := "community.lexicon.calendar.event#scheduled"
	if e.Status == "cancelled" {
		status = "community.lexicon.calendar.event#cancelled"
	}

	startTime := e.Start.DateTime
	if startTime == "" {
		startTime = e.Start.Date
	}
	endTime := e.End.DateTime
	if endTime == "" {
		endTime = e.End.Date
	}

	event := CalendarEvent{
		ID:          e.Id,
		Name:        e.Summary,
		Description: e.Description,
		CreatedAt:   e.Created,
		StartsAt:    startTime,
		EndsAt:      endTime,
		Status:      status,
	}

	if e.Location != "" {
		event.Locations = []CalendarEventLocation{
			{
				Type: "community.lexicon.calendar.event#uri",
				URI:  e.Location,
			},
		}
	}

	return event
}
