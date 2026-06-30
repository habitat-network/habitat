package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	"github.com/habitat-network/habitat/internal/sap"
)

type memberRecord struct {
	DisplayName   string `json:"displayName"`
	FunFact       string `json:"funFact"`
	FavoriteFruit string `json:"favoriteFruit"`
	CreatedAt     string `json:"createdAt"`
}

type chatRecord struct {
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

type chatReplyRecord struct {
	Text      string `json:"text"`
	ReplyTo   string `json:"replyTo"`
	CreatedAt string `json:"createdAt"`
}

type logRecord struct {
	Fruit     string `json:"fruit"`
	Count     int    `json:"count"`
	CreatedAt string `json:"createdAt"`
}

func Run(ctx context.Context, outbox sap.Outbox, store *index.Store) error {
	watch := outbox.Watch()
	for {
		msgs, err := outbox.Poll(ctx, 50)
		if err != nil {
			return fmt.Errorf("poll outbox: %w", err)
		}
		for _, msg := range msgs {
			if err := process(msg, store); err != nil {
				slog.WarnContext(ctx, "skipping unprocessable outbox message", "uri", msg.URI, "err", err)
			}
			if err := outbox.Ack(ctx, msg.ID); err != nil {
				return fmt.Errorf("ack outbox message %d: %w", msg.ID, err)
			}
		}
		if len(msgs) == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-watch:
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func process(msg sap.OutboxMessage, store *index.Store) error {
	collection := string(msg.URI.Collection())
	did := string(msg.URI.Repo())
	uri := msg.URI.String()

	switch collection {
	case "community.fruitgang.member":
		var r memberRecord
		if err := json.Unmarshal(msg.Value, &r); err != nil {
			return fmt.Errorf("decode member: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		return store.UpsertMember(index.Member{
			URI: uri, DID: did, DisplayName: r.DisplayName,
			FunFact: r.FunFact, FavoriteFruit: r.FavoriteFruit, CreatedAt: t,
		})

	case "community.fruitgang.chat":
		var r chatRecord
		if err := json.Unmarshal(msg.Value, &r); err != nil {
			return fmt.Errorf("decode chat: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		return store.UpsertChat(index.Chat{URI: uri, AuthorDID: did, Text: r.Text, CreatedAt: t})

	case "community.fruitgang.chatReply":
		var r chatReplyRecord
		if err := json.Unmarshal(msg.Value, &r); err != nil {
			return fmt.Errorf("decode chatReply: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		return store.UpsertChatReply(index.ChatReply{
			URI: uri, AuthorDID: did, ReplyTo: r.ReplyTo, Text: r.Text, CreatedAt: t,
		})

	case "community.fruitgang.log":
		var r logRecord
		if err := json.Unmarshal(msg.Value, &r); err != nil {
			return fmt.Errorf("decode log: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, r.CreatedAt)
		return store.UpsertLog(index.Log{
			URI: uri, AuthorDID: did, Fruit: r.Fruit, Count: r.Count, CreatedAt: t,
		})

	default:
		// Not a fruitgang record — ignore silently
		return nil
	}
}
