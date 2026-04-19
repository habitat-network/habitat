package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type GoogleClient struct {
	config         *oauth2.Config
	redirectURI    string
	tokenStore     *Store
	httpClientFunc func(ctx context.Context, session *Session) (*oauth2.Token, error)
}

func NewGoogleClient(clientID, clientSecret, redirectURI string, store *Store) *GoogleClient {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes: []string{
			"https://www.googleapis.com/auth/calendar.readonly",
			"https://www.googleapis.com/auth/userinfo.email",
		},
		Endpoint: google.Endpoint,
	}

	return &GoogleClient{
		config:      config,
		redirectURI: redirectURI,
		tokenStore:  store,
	}
}

func (gc *GoogleClient) AuthCodeURL(state string) string {
	return gc.config.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

func (gc *GoogleClient) Exchange(ctx context.Context, code string) (*oauth2.Token, string, error) {
	tok, err := gc.config.Exchange(ctx, code)
	if err != nil {
		return nil, "", fmt.Errorf("exchanging code: %w", err)
	}

	raw := tok.WithExtra(map[string]interface{}{})
	accessToken, _ := raw.Extra("access_token").(string)

	return tok, accessToken, nil
}

func (gc *GoogleClient) GetUserEmail(ctx context.Context, accessToken string) (string, error) {
	client := gc.config.Client(ctx, &oauth2.Token{AccessToken: accessToken})
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", fmt.Errorf("fetching user info: %w", err)
	}
	defer resp.Body.Close()

	var userInfo struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return "", fmt.Errorf("decoding user info: %w", err)
	}

	return userInfo.Email, nil
}

func (gc *GoogleClient) GetCalendarService(ctx context.Context, session *Session) (*calendar.Service, error) {
	tok, err := gc.getValidToken(ctx, session)
	if err != nil {
		return nil, err
	}

	client := gc.config.Client(ctx, tok)
	return calendar.NewService(ctx, option.WithHTTPClient(client))
}

func (gc *GoogleClient) getValidToken(ctx context.Context, session *Session) (*oauth2.Token, error) {
	if session.GoogleAccessToken == "" {
		return nil, fmt.Errorf("no access token available")
	}

	now := time.Now().Unix()
	if session.TokenExpiry > now {
		return &oauth2.Token{
			AccessToken:  session.GoogleAccessToken,
			RefreshToken: session.GoogleRefreshToken,
			Expiry:       time.Unix(session.TokenExpiry, 0),
		}, nil
	}

	if session.GoogleRefreshToken == "" {
		return nil, fmt.Errorf("token expired and no refresh token available")
	}

	tok, err := gc.config.TokenSource(ctx, &oauth2.Token{
		RefreshToken: session.GoogleRefreshToken,
	}).Token()
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}

	if err := gc.tokenStore.UpdateTokens(
		session.ID,
		tok.AccessToken,
		tok.RefreshToken,
		tok.Expiry.Unix(),
	); err != nil {
		return nil, fmt.Errorf("storing refreshed token: %w", err)
	}

	return tok, nil
}

func (gc *GoogleClient) ListEvents(ctx context.Context, session *Session, calendarID string, timeMin, timeMax string, pageSize int64) ([]*calendar.Event, error) {
	svc, err := gc.GetCalendarService(ctx, session)
	if err != nil {
		return nil, err
	}

	if calendarID == "" {
		calendarID = "primary"
	}
	if pageSize == 0 {
		pageSize = 100
	}

	events := []*calendar.Event{}
	pageToken := ""

	for {
		call := svc.Events.List(calendarID).
			ShowDeleted(false).
			SingleEvents(true).
			OrderBy("startTime").
			MaxResults(pageSize)

		if timeMin != "" {
			call = call.TimeMin(timeMin)
		}
		if timeMax != "" {
			call = call.TimeMax(timeMax)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		eventList, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("listing events: %w", err)
		}

		events = append(events, eventList.Items...)

		pageToken = eventList.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return events, nil
}

func (gc *GoogleClient) GetEvent(ctx context.Context, session *Session, calendarID, eventID string) (*calendar.Event, error) {
	svc, err := gc.GetCalendarService(ctx, session)
	if err != nil {
		return nil, err
	}

	if calendarID == "" {
		calendarID = "primary"
	}

	event, err := svc.Events.Get(calendarID, eventID).Do()
	if err != nil {
		return nil, fmt.Errorf("getting event: %w", err)
	}

	return event, nil
}
