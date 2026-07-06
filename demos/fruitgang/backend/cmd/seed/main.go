// seed is a bulk data-generation CLI for the Fruit Gang demo. It writes
// directly to the pear database (to mint real member identities and space
// records) and to the fruitgang backend's own index database (so the
// frontend reflects the new data immediately, without waiting on sap/the
// indexer to catch up).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/urfave/cli/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/demos/fruitgang/internal/index"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// memberPassword is the fixed password set on every member identity this
// tool mints. Fine for a demo where members are never logged into for real.
const memberPassword = "password"

var fruitKeys = []string{
	"greenApple", "redApple", "pear", "tangerine", "lemon", "lime", "banana",
	"watermelon", "grapes", "strawberry", "blueberry", "melon", "cherries",
	"peach", "mango", "pineapple", "coconut", "kiwi", "tomato", "olive", "avocado",
}

func validateFruit(fruit string) error {
	if fruit == "" || slices.Contains(fruitKeys, fruit) {
		return nil
	}
	return fmt.Errorf("unknown fruit %q, must be one of %v", fruit, fruitKeys)
}

// env wires up direct access to the pear and fruitgang databases, mirroring
// how cmd/pear/main.go constructs its stores (minus the HTTP server).
type env struct {
	orgDID    syntax.DID
	org       org.Store
	spaces    spaces.Store
	fruitgang *index.Store
	spaceURI  habitat_syntax.SpaceURI
}

func setup(ctx context.Context, cmd *cli.Command) (*env, error) {
	pearDBPath := cmd.String("pear-db")
	fgDBPath := cmd.String("fg-db")
	orgDIDStr := cmd.String("org")
	pearDomain := cmd.String("pear-domain")
	hiveDomain := cmd.String("hive-domain")
	if hiveDomain == "" {
		hiveDomain = pearDomain
	}

	orgDID, err := syntax.ParseDID(orgDIDStr)
	if err != nil {
		return nil, fmt.Errorf("parse --org: %w", err)
	}

	pearDB, err := gorm.Open(sqlite.Open(pearDBPath+"?_journal_mode=WAL"), &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open pear db: %w", err)
	}

	// Same sqlite-file-per-store convention as cmd/pear/main.go's setupFGA:
	// FGA needs its own file since it uses a different sqlite driver than GORM.
	fga, err := fgastore.NewSQLite(ctx, pearDBPath+".fga.db")
	if err != nil {
		return nil, fmt.Errorf("open fga store: %w", err)
	}

	hv, err := hive.NewHive(hiveDomain, pearDomain, pearDB)
	if err != nil {
		return nil, fmt.Errorf("setup hive: %w", err)
	}

	oauthSecret, err := encrypt.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate throwaway signing key: %w", err)
	}
	secretBytes, err := encrypt.ParseKey(oauthSecret)
	if err != nil {
		return nil, fmt.Errorf("parse throwaway signing key: %w", err)
	}
	// hive itself satisfies identity.Directory, so it doubles as the
	// directory dependency below -- no real network DID resolution needed
	// since every identity in this demo is hive-owned.
	passwordProvider, err := login.NewPasswordProvider(pearDB, pearDomain, secretBytes, hv)
	if err != nil {
		return nil, fmt.Errorf("setup password provider: %w", err)
	}

	orgStore, err := org.NewStore(pearDB, hv, hv, pearDomain, passwordProvider, fga)
	if err != nil {
		return nil, fmt.Errorf("setup org store: %w", err)
	}

	eventStore, err := events.NewStore(pearDB)
	if err != nil {
		return nil, fmt.Errorf("setup event store: %w", err)
	}
	spacesStore, err := spaces.NewStore(pearDB, fga, eventStore)
	if err != nil {
		return nil, fmt.Errorf("setup spaces store: %w", err)
	}

	fgDB, err := gorm.Open(sqlite.Open(fgDBPath+"?_journal_mode=WAL"), &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open fruitgang db: %w", err)
	}
	fgIndex, err := index.NewStore(fgDB)
	if err != nil {
		return nil, fmt.Errorf("setup fruitgang index store: %w", err)
	}

	spaceURI := habitat_syntax.ConstructSpaceURI(orgDID, syntax.NSID("network.habitat.group"), habitat_syntax.SpaceKey("fruitgang"))

	return &env{
		orgDID:    orgDID,
		org:       orgStore,
		spaces:    spacesStore,
		fruitgang: fgIndex,
		spaceURI:  spaceURI,
	}, nil
}

func sharedFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "pear-db", Sources: cli.EnvVars("PEAR_DB"), Required: true, Usage: "path to pear's sqlite db file"},
		&cli.StringFlag{Name: "fg-db", Sources: cli.EnvVars("FG_DB"), Required: true, Usage: "path to the fruitgang backend's sqlite db file"},
		&cli.StringFlag{Name: "org", Sources: cli.EnvVars("ORG_DID"), Required: true, Usage: "DID of the connected org, e.g. did:web:acme.example"},
		&cli.StringFlag{Name: "pear-domain", Sources: cli.EnvVars("PEAR_DOMAIN"), Value: "pear.local.habitat.network", Usage: "domain pear is running on"},
		&cli.StringFlag{Name: "hive-domain", Sources: cli.EnvVars("HIVE_DOMAIN"), Usage: "hive member domain, defaults to --pear-domain"},
	}
}

func main() {
	cmd := &cli.Command{
		Name:  "seed",
		Usage: "bulk-create Fruit Gang members and records by writing directly to the pear and fruitgang databases",
		Commands: []*cli.Command{
			createMemberCommand(),
			postCommand(),
			replyCommand(),
			logCommand(),
			addToGroupCommand(),
		},
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		slog.Error("seed command failed", "err", err)
		os.Exit(1)
	}
}

func createMemberCommand() *cli.Command {
	return &cli.Command{
		Name:  "create-member",
		Usage: "mint a new member identity, join the org, and set their Fruit Gang profile",
		Flags: append(sharedFlags(),
			&cli.StringFlag{Name: "handle", Required: true, Usage: "internal handle prefix, e.g. \"alice\""},
			&cli.StringFlag{Name: "display-name", Required: true},
			&cli.StringFlag{Name: "fun-fact", Usage: "optional"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			e, err := setup(ctx, cmd)
			if err != nil {
				return err
			}

			token, err := e.org.IssueIdentityToken(ctx, e.orgDID, e.orgDID, true, time.Now().Add(time.Hour))
			if err != nil {
				return fmt.Errorf("issue identity token: %w", err)
			}
			id, err := e.org.CreateNewMemberIdentity(ctx, e.orgDID, token, cmd.String("handle"), memberPassword, "")
			if err != nil {
				return fmt.Errorf("create member identity: %w", err)
			}

			createdAt := time.Now().UTC()
			record := map[string]any{
				"displayName": cmd.String("display-name"),
				"createdAt":   createdAt.Format(time.RFC3339Nano),
			}
			if funFact := cmd.String("fun-fact"); funFact != "" {
				record["funFact"] = funFact
			}
			favoriteFruit := "community.fruitgang.member#" + fruitKeys[rand.IntN(len(fruitKeys))]
			record["favoriteFruit"] = favoriteFruit

			recordURI, _, err := e.spaces.PutRecord(
				ctx, e.spaceURI, id.DID, syntax.NSID("community.fruitgang.member"), syntax.RecordKey("self"), record,
			)
			if err != nil {
				return fmt.Errorf("put member record: %w", err)
			}

			if err := e.fruitgang.UpsertMember(index.Member{
				URI:           recordURI.String(),
				DID:           id.DID.String(),
				DisplayName:   cmd.String("display-name"),
				FunFact:       cmd.String("fun-fact"),
				FavoriteFruit: favoriteFruit,
				CreatedAt:     createdAt,
			}); err != nil {
				return fmt.Errorf("index member: %w", err)
			}

			fmt.Printf("created member %s (%s)\n", id.Handle, id.DID)
			return nil
		},
	}
}

func postCommand() *cli.Command {
	return &cli.Command{
		Name:  "post",
		Usage: "write a Fruit Gang chat post as an existing member",
		Flags: append(sharedFlags(),
			&cli.StringFlag{Name: "member", Required: true, Usage: "DID of the posting member"},
			&cli.StringFlag{Name: "text", Required: true},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			e, err := setup(ctx, cmd)
			if err != nil {
				return err
			}
			memberDID, err := syntax.ParseDID(cmd.String("member"))
			if err != nil {
				return fmt.Errorf("parse --member: %w", err)
			}

			createdAt := time.Now().UTC()
			record := map[string]any{
				"text":      cmd.String("text"),
				"createdAt": createdAt.Format(time.RFC3339Nano),
			}
			recordURI, _, err := e.spaces.PutRecord(
				ctx, e.spaceURI, memberDID, syntax.NSID("community.fruitgang.chat"), syntax.RecordKey(""), record,
			)
			if err != nil {
				return fmt.Errorf("put chat record: %w", err)
			}

			if err := e.fruitgang.UpsertChat(index.Chat{
				URI:       recordURI.String(),
				AuthorDID: memberDID.String(),
				Text:      cmd.String("text"),
				CreatedAt: createdAt,
			}); err != nil {
				return fmt.Errorf("index chat: %w", err)
			}

			fmt.Printf("posted %s\n", recordURI)
			return nil
		},
	}
}

func replyCommand() *cli.Command {
	return &cli.Command{
		Name:  "reply",
		Usage: "write a flat reply to a Fruit Gang chat post as an existing member",
		Flags: append(sharedFlags(),
			&cli.StringFlag{Name: "member", Required: true, Usage: "DID of the replying member"},
			&cli.StringFlag{Name: "text", Required: true},
			&cli.StringFlag{Name: "reply-to", Required: true, Usage: "AT URI of the community.fruitgang.chat post being replied to"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			e, err := setup(ctx, cmd)
			if err != nil {
				return err
			}
			memberDID, err := syntax.ParseDID(cmd.String("member"))
			if err != nil {
				return fmt.Errorf("parse --member: %w", err)
			}

			createdAt := time.Now().UTC()
			replyTo := cmd.String("reply-to")
			record := map[string]any{
				"text":      cmd.String("text"),
				"replyTo":   replyTo,
				"createdAt": createdAt.Format(time.RFC3339Nano),
			}
			recordURI, _, err := e.spaces.PutRecord(
				ctx, e.spaceURI, memberDID, syntax.NSID("community.fruitgang.chatReply"), syntax.RecordKey(""), record,
			)
			if err != nil {
				return fmt.Errorf("put chat reply record: %w", err)
			}

			if err := e.fruitgang.UpsertChatReply(index.ChatReply{
				URI:       recordURI.String(),
				AuthorDID: memberDID.String(),
				ReplyTo:   replyTo,
				Text:      cmd.String("text"),
				CreatedAt: createdAt,
			}); err != nil {
				return fmt.Errorf("index chat reply: %w", err)
			}

			fmt.Printf("replied %s\n", recordURI)
			return nil
		},
	}
}

func logCommand() *cli.Command {
	return &cli.Command{
		Name:  "log",
		Usage: "write a Fruit Gang fruit log entry as an existing member",
		Flags: append(sharedFlags(),
			&cli.StringFlag{Name: "member", Required: true, Usage: "DID of the logging member"},
			&cli.StringFlag{Name: "fruit", Required: true, Usage: fmt.Sprintf("one of: %v", fruitKeys)},
			&cli.IntFlag{Name: "count", Value: 1, Usage: "1-99"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fruit := cmd.String("fruit")
			if err := validateFruit(fruit); err != nil {
				return err
			}
			count := cmd.Int("count")
			if count < 1 || count > 99 {
				return fmt.Errorf("--count must be between 1 and 99, got %d", count)
			}

			e, err := setup(ctx, cmd)
			if err != nil {
				return err
			}
			memberDID, err := syntax.ParseDID(cmd.String("member"))
			if err != nil {
				return fmt.Errorf("parse --member: %w", err)
			}

			createdAt := time.Now().UTC()
			fullFruit := "community.fruitgang.log#" + fruit
			record := map[string]any{
				"fruit":     fullFruit,
				"count":     count,
				"createdAt": createdAt.Format(time.RFC3339Nano),
			}
			recordURI, _, err := e.spaces.PutRecord(
				ctx, e.spaceURI, memberDID, syntax.NSID("community.fruitgang.log"), syntax.RecordKey(""), record,
			)
			if err != nil {
				return fmt.Errorf("put log record: %w", err)
			}

			if err := e.fruitgang.UpsertLog(index.Log{
				URI:       recordURI.String(),
				AuthorDID: memberDID.String(),
				Fruit:     fullFruit,
				Count:     int(count),
				CreatedAt: createdAt,
			}); err != nil {
				return fmt.Errorf("index log: %w", err)
			}

			fmt.Printf("logged %s\n", recordURI)
			return nil
		},
	}
}

func addToGroupCommand() *cli.Command {
	return &cli.Command{
		Name:  "add-to-group",
		Usage: "add an existing member to a pre-defined habitat group/space",
		Flags: append(sharedFlags(),
			&cli.StringFlag{Name: "member", Required: true, Usage: "DID of the member to add"},
			&cli.StringFlag{Name: "group", Required: true, Usage: "AT URI of the group/space to add them to"},
			&cli.StringFlag{Name: "access", Value: "write", Usage: "read or write"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			e, err := setup(ctx, cmd)
			if err != nil {
				return err
			}
			memberDID, err := syntax.ParseDID(cmd.String("member"))
			if err != nil {
				return fmt.Errorf("parse --member: %w", err)
			}
			groupURI, err := habitat_syntax.ParseSpaceURI(cmd.String("group"))
			if err != nil {
				return fmt.Errorf("parse --group: %w", err)
			}
			access, err := spaces.ParseSpaceAccess(cmd.String("access"))
			if err != nil {
				return err
			}

			if err := e.spaces.AddMember(ctx, groupURI, memberDID, access); err != nil {
				return fmt.Errorf("add member to group: %w", err)
			}

			fmt.Printf("added %s to %s (%s)\n", memberDID, groupURI, access)
			return nil
		},
	}
}
