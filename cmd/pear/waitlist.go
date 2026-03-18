package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/bradenaw/juniper/xmaps"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// Add waitlist signups to a google spreadsheet for easy access

type waitlistService struct {
	sheetID string
	svc     *sheets.Service
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
var source = xmaps.Set[string]{"index": {}, "user": {}, "developer": {}}

func getSheetsService(ctx context.Context, credsJSON string) (*sheets.Service, error) {
	creds, err := google.CredentialsFromJSONWithType(
		ctx,
		[]byte(credsJSON),
		google.ServiceAccount,
		sheets.SpreadsheetsScope,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	svc, err := sheets.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to create sheets service: %w", err)
	}

	return svc, nil
}

func NewWaitlistService(ctx context.Context, sheetID string, credsJSON string) (*waitlistService, error) {
	svc, err := getSheetsService(ctx, credsJSON)
	if err != nil {
		return nil, err
	}

	return &waitlistService{
		svc:     svc,
		sheetID: sheetID,
	}, nil
}

type request struct {
	Email string `json:"email"`
	From  string `json:"from"`
}

// Handle sign-ups to the waitlist
func (s *waitlistService) HandleWaitlistEmailSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req request
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	row := &sheets.ValueRange{
		Values: [][]interface{}{
			{req.Email, req.From},
		},
	}

	if !emailRegex.MatchString(req.Email) {
		http.Error(w, "invalid email address", http.StatusBadRequest)
		return
	}

	if !source.Contains(req.From) {
		http.Error(w, "invalid request source", http.StatusBadRequest)
		return
	}

	_, err := s.svc.Spreadsheets.Values.Append(s.sheetID, "Sheet1!A:A", row).
		ValueInputOption("USER_ENTERED").
		Do()
	if err != nil {
		http.Error(w, "failed to write to sheet", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("success"))
}
