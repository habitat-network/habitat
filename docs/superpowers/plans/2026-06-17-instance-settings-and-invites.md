# Instance Settings, Invite Links, and Org-Creation Enforcement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an instance admin configure the instance name and org-creation policy (open vs invite-only), generate single-use invite links, and have both the `pear` server and the `frontend` org-creation page enforce/honor that policy.

**Architecture:** Extend the existing `internal/instanceadmin` package (bootstrap admin auth from a prior pass) with a settings/invite store and three new admin-only XRPC procedures, plus a new public `describeInstance` query. `internal/org`'s `CreateOrg` gains a small consumer-defined `InstancePolicy` interface dependency to enforce the policy without importing `instanceadmin` directly. The frontend `org/create` page decodes an invite JWT (display only) or calls `describeInstance` against a manually-entered domain, and passes the token through to `org.create`.

**Tech Stack:** Go 1.26, gorm/sqlite (tests) + postgres (prod), `github.com/go-jose/go-jose/v3` for JWTs, `github.com/alexedwards/argon2id`, lexgen-generated lexicon types, React 19 + TanStack Router/Query, `internal`/`api` TypeScript packages.

## Global Constraints

- Lexicons live in `lexicons/` and are regenerated into `api/habitat/*.go` (Go) and `typescript/api/types/...` (TS) via `moon :generate` — never hand-edit generated files.
- New JSON fields/lexicon properties use `snake_case` (matches `network.habitat.org.create`'s `admin_handle`, `login_method`, etc — ignore the older camelCase `getMetadata` lexicon as an outlier).
- Auth checks in handlers use the `caller, ok := authn.Validate(w, r, s.auth); if !ok { return }`-style helper-at-top-of-handler pattern, not middleware wrapping.
- Errors: `errors.New`/`errors.Is` for reusable named errors, `fmt.Errorf` for one-offs; handle or return, never both; don't log and return the same error.
- All new Go tests use `gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})` per existing convention (see `internal/org/store_test.go`, `internal/instanceadmin/store_test.go`).
- Run `gofmt -w` on touched Go files before each commit; don't worry about golangci-lint nits (per user direction earlier in this project).

---

### Task 1: Lexicons for admin settings, invites, and describeInstance

**Files:**
- Create: `lexicons/network/habitat/admin/getSettings.json`
- Create: `lexicons/network/habitat/admin/updateSettings.json`
- Create: `lexicons/network/habitat/admin/issueInvite.json`
- Create: `lexicons/network/habitat/instance/describeInstance.json`
- Modify: `lexicons/network/habitat/org/create.json`

**Interfaces:**
- Produces (consumed by Task 4): generated Go types `habitat.NetworkHabitatAdminGetSettingsOutput`, `habitat.NetworkHabitatAdminUpdateSettingsInput`/`Output`, `habitat.NetworkHabitatAdminIssueInviteOutput`, `habitat.NetworkHabitatInstanceDescribeInstanceOutput`.
- Produces (consumed by Task 5): `habitat.NetworkHabitatOrgCreateInput.InviteToken` field.
- Produces (consumed by Task 7): TS types `NetworkHabitatInstanceDescribeInstance.QueryParams`/`OutputSchema` under the `api` package, and an updated `NetworkHabitatOrgCreate.InputSchema` with `invite_token`.

- [ ] **Step 1: Create the `admin.getSettings` lexicon**

```json
{
    "lexicon": 1,
    "id": "network.habitat.admin.getSettings",
    "defs": {
        "main": {
            "type": "query",
            "description": "Get this instance's admin-configurable settings. Requires an authenticated instance admin session.",
            "output": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": ["instanceName", "orgCreationPolicy"],
                    "properties": {
                        "instanceName": {
                            "type": "string",
                            "description": "This instance's display name."
                        },
                        "orgCreationPolicy": {
                            "type": "string",
                            "description": "'open' or 'invite_only'."
                        }
                    }
                }
            }
        }
    }
}
```

Save to `lexicons/network/habitat/admin/getSettings.json`.

- [ ] **Step 2: Create the `admin.updateSettings` lexicon**

```json
{
    "lexicon": 1,
    "id": "network.habitat.admin.updateSettings",
    "defs": {
        "main": {
            "type": "procedure",
            "description": "Update this instance's admin-configurable settings. Requires an authenticated instance admin session.",
            "input": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": ["instanceName", "orgCreationPolicy"],
                    "properties": {
                        "instanceName": {
                            "type": "string",
                            "description": "This instance's display name."
                        },
                        "orgCreationPolicy": {
                            "type": "string",
                            "description": "'open' or 'invite_only'."
                        }
                    }
                }
            },
            "output": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": ["instanceName", "orgCreationPolicy"],
                    "properties": {
                        "instanceName": {
                            "type": "string"
                        },
                        "orgCreationPolicy": {
                            "type": "string"
                        }
                    }
                }
            }
        }
    }
}
```

Save to `lexicons/network/habitat/admin/updateSettings.json`.

- [ ] **Step 3: Create the `admin.issueInvite` lexicon**

```json
{
    "lexicon": 1,
    "id": "network.habitat.admin.issueInvite",
    "defs": {
        "main": {
            "type": "procedure",
            "description": "Issue a single-use invite token for creating an org on this instance. Requires an authenticated instance admin session.",
            "output": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": ["token"],
                    "properties": {
                        "token": {
                            "type": "string",
                            "description": "Signed, single-use invite token to embed in an org-creation link."
                        }
                    }
                }
            }
        }
    }
}
```

Save to `lexicons/network/habitat/admin/issueInvite.json`.

- [ ] **Step 4: Create the `instance.describeInstance` lexicon**

```json
{
    "lexicon": 1,
    "id": "network.habitat.instance.describeInstance",
    "defs": {
        "main": {
            "type": "query",
            "description": "Get public info about this instance. Modeled on com.atproto.server.describeServer. No authentication required.",
            "output": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": ["name", "inviteRequired"],
                    "properties": {
                        "name": {
                            "type": "string",
                            "description": "This instance's manager-configured display name."
                        },
                        "inviteRequired": {
                            "type": "boolean",
                            "description": "Whether creating an org on this instance requires an invite token."
                        }
                    }
                }
            }
        }
    }
}
```

Save to `lexicons/network/habitat/instance/describeInstance.json`.

- [ ] **Step 5: Add `invite_token` to `org.create`'s input**

Read `lexicons/network/habitat/org/create.json` first, then add a new property to the existing `input.schema.properties` object (it is not in `required`):

```json
                        "invite_token": {
                            "type": "string",
                            "description": "Single-use invite token from an instance admin, required when the instance's org creation policy is invite_only."
                        }
```

- [ ] **Step 6: Regenerate Go and TypeScript lexicon types**

Run: `moon :generate`
Expected: succeeds with no errors. Then verify the new files exist:

Run: `ls api/habitat/admin_getSettings.go api/habitat/admin_updateSettings.go api/habitat/admin_issueInvite.go api/habitat/instance_describeInstance.go`
Expected: all four files listed (no "No such file" errors).

Run: `grep -n "InviteToken" api/habitat/org_create.go`
Expected: a line like `InviteToken string \`json:"invite_token,omitempty"\`` in `NetworkHabitatOrgCreateInput`.

Run: `find typescript/api/types/network/habitat/admin typescript/api/types/network/habitat/instance`
Expected: `getSettings.ts`, `updateSettings.ts`, `issueInvite.ts` under `admin/`, and `describeInstance.ts` under `instance/`.

- [ ] **Step 7: Commit**

```bash
git add lexicons/network/habitat/admin lexicons/network/habitat/instance lexicons/network/habitat/org/create.json api/habitat typescript/api
git commit -m "$(cat <<'EOF'
Add lexicons for instance admin settings/invites and describeInstance

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Instance settings store (`internal/instanceadmin`)

**Files:**
- Modify: `internal/instanceadmin/models.go`
- Modify: `internal/instanceadmin/store.go`
- Modify: `internal/instanceadmin/store_test.go`

**Interfaces:**
- Consumes: existing `storeImpl` struct and `NewStore(db *gorm.DB) (Store, error)` from `internal/instanceadmin/store.go`.
- Produces (consumed by Task 4): `Store.GetSettings(ctx) (instanceName, orgCreationPolicy string, err error)`, `Store.UpdateSettings(ctx, instanceName, orgCreationPolicy string) error`, exported `ErrInvalidPolicy`.
- Produces (consumed by Task 5): `Store.GetOrgCreationPolicy(ctx context.Context) (string, error)`.

- [ ] **Step 1: Write the failing tests**

Read `internal/instanceadmin/store_test.go` first (to append, not replace). Add these tests at the end of the file:

```go
func TestGetSettings_DefaultsOnFirstAccess(t *testing.T) {
	s := newTestStore(t)

	instanceName, policy, err := s.GetSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "", instanceName)
	require.Equal(t, "open", policy)
}

func TestUpdateSettings_PersistsAndReads(t *testing.T) {
	s := newTestStore(t)

	err := s.UpdateSettings(t.Context(), "Acme Hosting", "invite_only")
	require.NoError(t, err)

	instanceName, policy, err := s.GetSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Acme Hosting", instanceName)
	require.Equal(t, "invite_only", policy)
}

func TestUpdateSettings_RejectsInvalidPolicy(t *testing.T) {
	s := newTestStore(t)

	err := s.UpdateSettings(t.Context(), "Acme Hosting", "sometimes")
	require.ErrorIs(t, err, ErrInvalidPolicy)
}

func TestGetOrgCreationPolicy_DefaultsToOpen(t *testing.T) {
	s := newTestStore(t)

	policy, err := s.GetOrgCreationPolicy(t.Context())
	require.NoError(t, err)
	require.Equal(t, "open", policy)
}

func TestGetOrgCreationPolicy_ReflectsUpdate(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	policy, err := s.GetOrgCreationPolicy(t.Context())
	require.NoError(t, err)
	require.Equal(t, "invite_only", policy)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/instanceadmin/... -run 'TestGetSettings|TestUpdateSettings|TestGetOrgCreationPolicy' -v`
Expected: FAIL with `s.GetSettings undefined` / `s.UpdateSettings undefined` / `s.GetOrgCreationPolicy undefined` / `undefined: ErrInvalidPolicy` compile errors.

- [ ] **Step 3: Add the `instanceSettings` model**

Read `internal/instanceadmin/models.go` first. Add:

```go
// instanceSettingsID is the fixed primary key for the single instance settings row.
const instanceSettingsID = 1

// instanceSettings holds the instance-wide settings configurable from the admin page.
type instanceSettings struct {
	ID                uint `gorm:"primaryKey"`
	InstanceName       string
	OrgCreationPolicy string
	SigningSecret     string
	CreatedAt         time.Time
}
```

- [ ] **Step 4: Implement `GetSettings`/`UpdateSettings`/`GetOrgCreationPolicy`**

Read `internal/instanceadmin/store.go` first. Add `"fmt"` to the import block, add to the `Store` interface (after `DeleteSession`):

```go
	// GetSettings returns the instance's current name and org creation
	// policy, creating the settings row with defaults on first access.
	GetSettings(ctx context.Context) (instanceName, orgCreationPolicy string, err error)

	// UpdateSettings updates the instance's name and org creation policy.
	// orgCreationPolicy must be "open" or "invite_only".
	UpdateSettings(ctx context.Context, instanceName, orgCreationPolicy string) error

	// GetOrgCreationPolicy is a convenience accessor for just the policy field.
	GetOrgCreationPolicy(ctx context.Context) (string, error)
```

Add a package-level error and the `policyOpen`/`policyInviteOnly` constants near `sessionDuration`:

```go
const (
	policyOpen       = "open"
	policyInviteOnly = "invite_only"
)

var ErrInvalidPolicy = errors.New("invalid org creation policy")
```

Add `&instanceSettings{}` to the `AutoMigrate` call in `NewStore`:

```go
	if err := db.AutoMigrate(&instanceAdminAccount{}, &instanceAdminSession{}, &instanceSettings{}); err != nil {
```

Add the implementation (a private helper that creates-on-read, plus the three public methods):

```go
func (s *storeImpl) getOrCreateSettings(ctx context.Context) (*instanceSettings, error) {
	var settings instanceSettings
	err := s.db.WithContext(ctx).First(&settings, instanceSettingsID).Error
	if err == nil {
		return &settings, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	secret, err := generateSigningSecret()
	if err != nil {
		return nil, err
	}
	settings = instanceSettings{
		ID:                instanceSettingsID,
		OrgCreationPolicy: policyOpen,
		SigningSecret:     secret,
		CreatedAt:         time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(&settings).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

func generateSigningSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (s *storeImpl) GetSettings(ctx context.Context) (string, string, error) {
	settings, err := s.getOrCreateSettings(ctx)
	if err != nil {
		return "", "", err
	}
	return settings.InstanceName, settings.OrgCreationPolicy, nil
}

func (s *storeImpl) UpdateSettings(ctx context.Context, instanceName, orgCreationPolicy string) error {
	if orgCreationPolicy != policyOpen && orgCreationPolicy != policyInviteOnly {
		return ErrInvalidPolicy
	}
	if _, err := s.getOrCreateSettings(ctx); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&instanceSettings{}).
		Where("id = ?", instanceSettingsID).
		Updates(map[string]any{
			"instanceName":        instanceName,
			"orgCreationPolicy": orgCreationPolicy,
		}).Error
}

func (s *storeImpl) GetOrgCreationPolicy(ctx context.Context) (string, error) {
	settings, err := s.getOrCreateSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.OrgCreationPolicy, nil
}
```

`fmt` is not actually needed — skip adding it; the imports above only need what's already imported (`context`, `crypto/rand`, `encoding/base64`, `errors`, `time`, `gorm.io/gorm`), all already present in `store.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/instanceadmin/... -v`
Expected: PASS, all tests including the new ones and the existing 15 from before.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/instanceadmin/models.go internal/instanceadmin/store.go
git add internal/instanceadmin/models.go internal/instanceadmin/store.go internal/instanceadmin/store_test.go
git commit -m "$(cat <<'EOF'
Add instance settings (name + org creation policy) to instanceadmin store

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Invite issuance and redemption (`internal/instanceadmin`)

**Files:**
- Modify: `internal/instanceadmin/models.go`
- Modify: `internal/instanceadmin/store.go`
- Modify: `internal/instanceadmin/store_test.go`

**Interfaces:**
- Consumes: `getOrCreateSettings(ctx) (*instanceSettings, error)` and `instanceSettings.SigningSecret`/`.InstanceName` from Task 2; `domain string` field to be added to `storeImpl`.
- Produces (consumed by Task 4): `Store.IssueInvite(ctx context.Context) (token string, err error)`.
- Produces (consumed by Task 5, via the `Store` interface satisfying `org.InstancePolicy`): `Store.RedeemInvite(ctx context.Context, token string) error`, exported `ErrInvalidInvite`.
- Produces (consumed by Task 6): `NewStore(db *gorm.DB, domain string) (Store, error)` — signature change, callers must be updated.

- [ ] **Step 1: Write the failing tests**

Read `internal/instanceadmin/store_test.go` first. Update `newTestStore` to pass a domain (it currently calls `NewStore(db)` with one argument):

```go
func newTestStore(t *testing.T) Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	store, err := NewStore(db, "pear.example.com")
	require.NoError(t, err)
	return store
}
```

Then append these tests:

```go
func TestIssueInvite_CanBeRedeemedOnce(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	token, err := s.IssueInvite(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, token)

	require.NoError(t, s.RedeemInvite(t.Context(), token))

	err = s.RedeemInvite(t.Context(), token)
	require.ErrorIs(t, err, ErrInvalidInvite)
}

func TestRedeemInvite_UnknownTokenFails(t *testing.T) {
	s := newTestStore(t)

	err := s.RedeemInvite(t.Context(), "not-a-real-token")
	require.ErrorIs(t, err, ErrInvalidInvite)
}

func TestRedeemInvite_WrongSigningSecretFails(t *testing.T) {
	s1 := newTestStore(t)
	require.NoError(t, s1.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))
	token, err := s1.IssueInvite(t.Context())
	require.NoError(t, err)

	// A different store (different generated signing secret) must reject it.
	s2 := newTestStore(t)
	err = s2.RedeemInvite(t.Context(), token)
	require.ErrorIs(t, err, ErrInvalidInvite)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/instanceadmin/... -run 'TestIssueInvite|TestRedeemInvite' -v`
Expected: FAIL — compile errors (`s.IssueInvite undefined`, `s.RedeemInvite undefined`, `undefined: ErrInvalidInvite`, and `NewStore(db)` arg-count mismatch since `newTestStore` was updated to pass two args before `NewStore` accepts them).

- [ ] **Step 3: Add the `instanceInvite` model**

In `internal/instanceadmin/models.go`, add:

```go
// instanceInvite is a single-use token allowing one org to be created on this
// instance when orgCreationPolicy is invite_only. Token is the JWT's jti claim,
// not the JWT itself.
type instanceInvite struct {
	Token     string `gorm:"primaryKey"`
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}
```

- [ ] **Step 4: Implement `IssueInvite`/`RedeemInvite` and update `NewStore`**

In `internal/instanceadmin/store.go`:

Update imports to add `"encoding/hex"` (already imported for sessions) — no change needed there. Add:

```go
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
```

Add to the `Store` interface:

```go
	// IssueInvite creates and signs a new single-use invite token.
	IssueInvite(ctx context.Context) (token string, err error)

	// RedeemInvite validates and consumes an invite token. All failure modes
	// (unknown, expired, already used, bad signature) return ErrInvalidInvite.
	RedeemInvite(ctx context.Context, token string) error
```

Add `ErrInvalidInvite` next to `ErrInvalidPolicy`, and the invite expiry constant next to `sessionDuration`:

```go
const inviteDuration = 7 * 24 * time.Hour

var ErrInvalidInvite = errors.New("invalid or expired invite")
```

Change `storeImpl` to carry the instance domain, and `NewStore`'s signature:

```go
type storeImpl struct {
	db     *gorm.DB
	domain string
}

var _ Store = (*storeImpl)(nil)

func NewStore(db *gorm.DB, domain string) (Store, error) {
	if err := db.AutoMigrate(
		&instanceAdminAccount{},
		&instanceAdminSession{},
		&instanceSettings{},
		&instanceInvite{},
	); err != nil {
		return nil, err
	}
	return &storeImpl{db: db, domain: domain}, nil
}
```

Add the claims type and the two methods:

```go
type inviteClaims struct {
	jwt.Claims
	Domain string `json:"domain"`
	Name   string `json:"name"`
}

func (s *storeImpl) IssueInvite(ctx context.Context) (string, error) {
	settings, err := s.getOrCreateSettings(ctx)
	if err != nil {
		return "", err
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	jti := hex.EncodeToString(b)
	now := time.Now()
	expiresAt := now.Add(inviteDuration)

	invite := instanceInvite{
		Token:     jti,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&invite).Error; err != nil {
		return "", err
	}

	secret, err := base64.StdEncoding.DecodeString(settings.SigningSecret)
	if err != nil {
		return "", err
	}
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: secret}, nil)
	if err != nil {
		return "", err
	}
	return jwt.Signed(sig).Claims(inviteClaims{
		Claims: jwt.Claims{
			ID:     jti,
			Expiry: jwt.NewNumericDate(expiresAt),
		},
		Domain: s.domain,
		Name:   settings.InstanceName,
	}).CompactSerialize()
}

func (s *storeImpl) RedeemInvite(ctx context.Context, token string) error {
	settings, err := s.getOrCreateSettings(ctx)
	if err != nil {
		return err
	}
	secret, err := base64.StdEncoding.DecodeString(settings.SigningSecret)
	if err != nil {
		return err
	}

	parsed, err := jwt.ParseSigned(token)
	if err != nil {
		return ErrInvalidInvite
	}
	var claims inviteClaims
	if err := parsed.Claims(secret, &claims); err != nil {
		return ErrInvalidInvite
	}
	if err := claims.Claims.ValidateWithLeeway(jwt.Expected{Time: time.Now()}, 0); err != nil {
		return ErrInvalidInvite
	}

	var invite instanceInvite
	err = s.db.WithContext(ctx).Where("token = ?", claims.ID).First(&invite).Error
	if err != nil {
		return ErrInvalidInvite
	}
	if invite.UsedAt != nil || invite.ExpiresAt.Before(time.Now()) {
		return ErrInvalidInvite
	}

	now := time.Now()
	return s.db.WithContext(ctx).Model(&instanceInvite{}).
		Where("token = ?", invite.Token).
		Update("used_at", &now).Error
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/instanceadmin/... -v`
Expected: PASS for all tests in the package.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/instanceadmin/models.go internal/instanceadmin/store.go
git add internal/instanceadmin/models.go internal/instanceadmin/store.go internal/instanceadmin/store_test.go
git commit -m "$(cat <<'EOF'
Add invite token issuance and redemption to instanceadmin store

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Admin HTTP handlers + describeInstance + settings UI (`internal/instanceadmin`)

**Files:**
- Modify: `internal/instanceadmin/server.go`
- Modify: `internal/instanceadmin/server_test.go`
- Modify: `internal/instanceadmin/home.html`

**Interfaces:**
- Consumes: `Store.GetSettings`/`UpdateSettings`/`IssueInvite`/`ErrInvalidPolicy` (Task 2/3); `habitat.NetworkHabitatAdminGetSettingsOutput`, `NetworkHabitatAdminUpdateSettingsInput/Output`, `NetworkHabitatAdminIssueInviteOutput`, `NetworkHabitatInstanceDescribeInstanceOutput` (Task 1).
- Produces (consumed by Task 6): `NewServer(store Store, frontendDomain string) *Server` — signature change. New handlers `GetSettings`, `UpdateSettings`, `IssueInvite`, `DescribeInstance` (all `http.HandlerFunc`-shaped methods on `*Server`).

- [ ] **Step 1: Write the failing tests**

Read `internal/instanceadmin/server_test.go` first. Update `newTestServer` (it currently does `NewServer(store)`) to pass a frontend domain:

```go
func newTestServer(t *testing.T) (*Server, Store, string) {
	t.Helper()
	store := newTestStore(t)
	password, created, err := store.Bootstrap(t.Context(), "")
	require.NoError(t, err)
	require.True(t, created)
	return NewServer(store, "frontend.example.com"), store, password
}
```

Append these tests:

```go
func TestGetSettings_RequiresSession(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.admin.getSettings", nil)
	rec := httptest.NewRecorder()
	server.GetSettings(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetSettings_ReturnsDefaults(t *testing.T) {
	server, store, _ := newTestServer(t)
	token, _, err := store.CreateSession(t.Context())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.admin.getSettings", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	server.GetSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatAdminGetSettingsOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Equal(t, "open", out.OrgCreationPolicy)
}

func TestUpdateSettings_PersistsAndReturnsUpdated(t *testing.T) {
	server, store, _ := newTestServer(t)
	token, _, err := store.CreateSession(t.Context())
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatAdminUpdateSettingsInput{
		InstanceName:       "Acme Hosting",
		OrgCreationPolicy: "invite_only",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.admin.updateSettings",
		bytes.NewReader(body),
	)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	server.UpdateSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatAdminUpdateSettingsOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Equal(t, "Acme Hosting", out.InstanceName)
	require.Equal(t, "invite_only", out.OrgCreationPolicy)
}

func TestUpdateSettings_InvalidPolicyReturns400(t *testing.T) {
	server, store, _ := newTestServer(t)
	token, _, err := store.CreateSession(t.Context())
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatAdminUpdateSettingsInput{
		InstanceName:       "Acme Hosting",
		OrgCreationPolicy: "sometimes",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.admin.updateSettings",
		bytes.NewReader(body),
	)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	server.UpdateSettings(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestIssueInvite_ReturnsToken(t *testing.T) {
	server, store, _ := newTestServer(t)
	token, _, err := store.CreateSession(t.Context())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.admin.issueInvite", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	server.IssueInvite(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatAdminIssueInviteOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.NotEmpty(t, out.Token)
}

func TestDescribeInstance_NoAuthRequired(t *testing.T) {
	server, store, _ := newTestServer(t)
	require.NoError(t, store.UpdateSettings(t.Context(), "Acme Hosting", "invite_only"))

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.instance.describeInstance", nil)
	rec := httptest.NewRecorder()
	server.DescribeInstance(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out habitat.NetworkHabitatInstanceDescribeInstanceOutput
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	require.Equal(t, "Acme Hosting", out.Name)
	require.True(t, out.InviteRequired)
}
```

Add `"bytes"` and `"encoding/json"` and `"github.com/habitat-network/habitat/api/habitat"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/instanceadmin/... -v`
Expected: FAIL — compile errors (`server.GetSettings undefined`, etc., and `NewServer(store)` arg mismatch from the updated `newTestServer`).

- [ ] **Step 3: Implement the session-check helpers and handlers**

Read `internal/instanceadmin/server.go` first. Replace the `Server` struct, `NewServer`, and the `RequireSession` method with:

```go
// Server serves the instance admin login/logout flow, the admin settings
// page, and gates access to instance admin routes via session cookies.
type Server struct {
	store          Store
	frontendDomain string
}

func NewServer(store Store, frontendDomain string) *Server {
	return &Server{store: store, frontendDomain: frontendDomain}
}
```

Replacing `RequireSession` means the existing tests that call it directly no longer compile. Read `internal/instanceadmin/server_test.go` first, then replace `TestRequireSession_RedirectsWithoutCookie` and `TestRequireSession_PassesThroughWithValidCookie` (the two tests that call `server.RequireSession(...)`):

```go
func TestRequireSession_RedirectsWithoutCookie(t *testing.T) {
	server, _, _ := newTestServer(t)

	called := false
	handler := server.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	require.False(t, called)
	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/admin/login", rec.Header().Get("Location"))
}

func TestRequireSession_PassesThroughWithValidCookie(t *testing.T) {
	server, store, _ := newTestServer(t)

	token, _, err := store.CreateSession(t.Context())
	require.NoError(t, err)

	called := false
	handler := server.RequireSession(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	handler(rec, req)

	require.True(t, called)
}
```

with:

```go
func TestRequireSessionAPI_FailsWithoutCookie(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.admin.getSettings", nil)
	rec := httptest.NewRecorder()
	ok := server.requireSessionAPI(rec, req)

	require.False(t, ok)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireSessionAPI_PassesWithValidCookie(t *testing.T) {
	server, store, _ := newTestServer(t)

	token, _, err := store.CreateSession(t.Context())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.admin.getSettings", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	ok := server.requireSessionAPI(rec, req)

	require.True(t, ok)
}
```

(`TestServeAdminHome_RedirectsWithoutSession`, added in a later step below, covers the `requireSessionPage` redirect case that `TestRequireSession_RedirectsWithoutCookie` used to cover.)

Now add the two new helpers — read the rest of `server.go` and replace the `RequireSession` method with:

```go
// requireSessionPage reports whether the request carries a valid instance
// admin session, redirecting to the login page and returning false if not.
func (s *Server) requireSessionPage(w http.ResponseWriter, r *http.Request) bool {
	ok, err := s.hasValidSession(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "validating instance admin session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return false
	}
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return false
	}
	return true
}

// requireSessionAPI reports whether the request carries a valid instance
// admin session, writing a 401 and returning false if not.
func (s *Server) requireSessionAPI(w http.ResponseWriter, r *http.Request) bool {
	ok, err := s.hasValidSession(r)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "validating instance admin session", http.StatusInternalServerError)
		return false
	}
	if !ok {
		utils.LogAndHTTPError(r.Context(), w, errors.New("not authenticated"), "checking instance admin session", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) hasValidSession(r *http.Request) (bool, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false, nil
	}
	return s.store.ValidateSession(r.Context(), cookie.Value)
}
```

Update `ServeAdminHome` to call the page helper and pass the frontend domain to the template:

```go
func (s *Server) ServeAdminHome(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionPage(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct{ FrontendDomain string }{s.frontendDomain}
	if err := templates.ExecuteTemplate(w, "home.html", data); err != nil {
		slog.Error("rendering instance admin home page", "err", err)
	}
}
```

Add the four new handlers at the end of the file:

```go
func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionAPI(w, r) {
		return
	}
	instanceName, policy, err := s.store.GetSettings(r.Context())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "getting instance settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, habitat.NetworkHabitatAdminGetSettingsOutput{
		InstanceName:       instanceName,
		OrgCreationPolicy: policy,
	})
}

func (s *Server) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionAPI(w, r) {
		return
	}
	var req habitat.NetworkHabitatAdminUpdateSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateSettings(r.Context(), req.InstanceName, req.OrgCreationPolicy); err != nil {
		if errors.Is(err, ErrInvalidPolicy) {
			utils.LogAndHTTPError(r.Context(), w, err, "updating instance settings", http.StatusBadRequest)
			return
		}
		utils.LogAndHTTPError(r.Context(), w, err, "updating instance settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, habitat.NetworkHabitatAdminUpdateSettingsOutput{
		InstanceName:       req.InstanceName,
		OrgCreationPolicy: req.OrgCreationPolicy,
	})
}

func (s *Server) IssueInvite(w http.ResponseWriter, r *http.Request) {
	if !s.requireSessionAPI(w, r) {
		return
	}
	token, err := s.store.IssueInvite(r.Context())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "issuing invite", http.StatusInternalServerError)
		return
	}
	writeJSON(w, habitat.NetworkHabitatAdminIssueInviteOutput{Token: token})
}

func (s *Server) DescribeInstance(w http.ResponseWriter, r *http.Request) {
	instanceName, policy, err := s.store.GetSettings(r.Context())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "getting instance settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, habitat.NetworkHabitatInstanceDescribeInstanceOutput{
		Name:           instanceName,
		InviteRequired: policy == policyInviteOnly,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
```

Add `"encoding/json"`, `"errors"`, `"github.com/habitat-network/habitat/api/habitat"`, and `"github.com/habitat-network/habitat/internal/utils"` to `server.go`'s imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/instanceadmin/... -v`
Expected: PASS for all tests in the package.

- [ ] **Step 5: Add the settings UI to `home.html`**

Read `internal/instanceadmin/home.html` first, then replace its `<body>` contents with a settings form and inline script:

```html
<body>
  <main class="card">
    <h1>Habitat</h1>
    <p class="subtitle">Instance settings</p>
    <div id="error" class="error" style="display:none"></div>
    <label for="instanceName">Instance name</label>
    <input type="text" id="instanceName">
    <label for="policy">Org creation</label>
    <select id="policy">
      <option value="open">Open</option>
      <option value="invite_only">Invite only</option>
    </select>
    <button id="save">Save settings</button>
    <div id="invite-section" style="display:none; margin-top: 1rem;">
      <button id="generate-invite" class="secondary" type="button">Generate invite link</button>
      <input type="text" id="invite-link" readonly style="margin-top: 0.5rem;">
    </div>
    <form method="POST" action="/admin/logout" style="margin-top: 1.5rem;">
      <button type="submit" class="secondary">Log out</button>
    </form>
  </main>
  <script>
    const frontendDomain = {{.FrontendDomain}};
    const errorEl = document.getElementById("error");
    const nameEl = document.getElementById("instanceName");
    const policyEl = document.getElementById("policy");
    const inviteSection = document.getElementById("invite-section");
    const inviteLinkEl = document.getElementById("invite-link");

    function showError(msg) {
      errorEl.textContent = msg;
      errorEl.style.display = "block";
    }

    function applyPolicy(policy) {
      inviteSection.style.display = policy === "invite_only" ? "block" : "none";
    }

    fetch("/xrpc/network.habitat.admin.getSettings")
      .then((r) => r.json())
      .then((data) => {
        nameEl.value = data.instanceName || "";
        policyEl.value = data.orgCreationPolicy;
        applyPolicy(data.orgCreationPolicy);
      })
      .catch(() => showError("failed to load settings"));

    policyEl.addEventListener("change", () => applyPolicy(policyEl.value));

    document.getElementById("save").addEventListener("click", () => {
      fetch("/xrpc/network.habitat.admin.updateSettings", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          instanceName: nameEl.value,
          orgCreationPolicy: policyEl.value,
        }),
      })
        .then((r) => {
          if (!r.ok) throw new Error("save failed");
          return r.json();
        })
        .then((data) => applyPolicy(data.orgCreationPolicy))
        .catch(() => showError("failed to save settings"));
    });

    document.getElementById("generate-invite").addEventListener("click", () => {
      fetch("/xrpc/network.habitat.admin.issueInvite", { method: "POST" })
        .then((r) => {
          if (!r.ok) throw new Error("issue failed");
          return r.json();
        })
        .then((data) => {
          inviteLinkEl.style.display = "block";
          inviteLinkEl.value = "https://" + frontendDomain + "/org/create?token=" + data.token;
        })
        .catch(() => showError("failed to generate invite link"));
    });
  </script>
</body>
```

Note `{{.FrontendDomain}}` is rendered inside a `<script>` block by Go's `html/template`, which JS-escapes it into a quoted string literal automatically — no manual quoting needed.

- [ ] **Step 6: Update `TestServeAdminHome` for the new copy and session requirement**

`ServeAdminHome` now calls `requireSessionPage`, and the page copy changed from "Logged in as instance admin" to "Instance settings". Read `internal/instanceadmin/server_test.go` first, then replace its existing `TestServeAdminHome`:

```go
func TestServeAdminHome(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	server.ServeAdminHome(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "instance admin")
}
```

with:

```go
func TestServeAdminHome(t *testing.T) {
	server, store, _ := newTestServer(t)
	token, _, err := store.CreateSession(t.Context())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	server.ServeAdminHome(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Instance settings")
}

func TestServeAdminHome_RedirectsWithoutSession(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	server.ServeAdminHome(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/admin/login", rec.Header().Get("Location"))
}
```

Run: `go test ./internal/instanceadmin/... -run TestServeAdminHome -v`
Expected: PASS for both tests.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/instanceadmin/server.go
git add internal/instanceadmin/server.go internal/instanceadmin/server_test.go internal/instanceadmin/home.html
git commit -m "$(cat <<'EOF'
Add admin settings/invite handlers, describeInstance, and settings UI

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Org-creation enforcement (`internal/org`)

**Files:**
- Modify: `internal/org/server.go`
- Modify: `internal/org/server_test.go`

**Interfaces:**
- Consumes: `habitat.NetworkHabitatOrgCreateInput.InviteToken` (Task 1).
- Produces (consumed by Task 6): exported `InstancePolicy` interface; `NewServer(store, auth, p, domain, dir, instancePolicy InstancePolicy) (*Server, error)` — signature change, callers must be updated.

- [ ] **Step 1: Write the failing tests**

Read `internal/org/server_test.go` first. Add a fake policy type and update `newTestServer` to pass it (it currently calls `NewServer(storeImpl, authn.NewStubAuthnForTest(adminDID), nil, "pear.example.com", identity.DefaultDirectory())`):

```go
type fakeInstancePolicy struct {
	policy           string
	redeemErr        error
	redeemedTokens   []string
}

func (f *fakeInstancePolicy) GetOrgCreationPolicy(ctx context.Context) (string, error) {
	return f.policy, nil
}

func (f *fakeInstancePolicy) RedeemInvite(ctx context.Context, token string) error {
	if f.redeemErr != nil {
		return f.redeemErr
	}
	f.redeemedTokens = append(f.redeemedTokens, token)
	return nil
}
```

Update `newTestServer`'s signature to accept a policy and thread it through:

```go
func newTestServer(
	t *testing.T,
	adminDID syntax.DID,
	policy InstancePolicy,
) (*Server, syntax.DID) {
	t.Helper()
	storeImpl := newTestStore(t)

	orgIdIdent, _, err := storeImpl.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"password",
		"password",
		"",
		"",
	)
	require.NoError(t, err)

	scoped, err := storeImpl.GetOrg(context.Background(), orgIdIdent.DID)
	require.NoError(t, err)
	st := scoped.(*orgImpl)
	require.NoError(t, st.addMemberTx(context.Background(), st.db, adminDID))
	require.NoError(t, st.AddAdmin(context.Background(), adminDID))

	srv, err := NewServer(
		storeImpl,
		authn.NewStubAuthnForTest(adminDID),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		policy,
	)
	require.NoError(t, err)
	return srv, orgIdIdent.DID
}
```

Find every other call site of `newTestServer(t, ...)` in `internal/org/server_test.go` (Run: `grep -n "newTestServer(t" internal/org/server_test.go`) and add `&fakeInstancePolicy{policy: "open"}` as the new last argument to each — this keeps existing tests passing under the open (no-op) policy.

Append these new tests:

```go
func TestCreateOrg_OpenPolicyIgnoresMissingToken(t *testing.T) {
	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "open"},
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.org.create", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestCreateOrg_InviteOnlyRejectsMissingToken(t *testing.T) {
	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "invite_only"},
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.org.create", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCreateOrg_InviteOnlyRejectsInvalidToken(t *testing.T) {
	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		&fakeInstancePolicy{policy: "invite_only", redeemErr: errors.New("bad token")},
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		InviteToken:     "garbage",
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.org.create", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCreateOrg_InviteOnlyAcceptsValidToken(t *testing.T) {
	policy := &fakeInstancePolicy{policy: "invite_only"}
	srv, err := NewServer(
		newTestStore(t),
		authn.NewStubAuthnForTest(did1),
		nil,
		"pear.example.com",
		identity.DefaultDirectory(),
		policy,
	)
	require.NoError(t, err)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgCreateInput{
		Name:            "test-org",
		AdminHandle:     "admin",
		AdminPassword:   "password",
		LoginMethod:     "password",
		HandleSubdomain: "test",
		InviteToken:     "a-valid-token",
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.org.create", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.CreateOrg(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []string{"a-valid-token"}, policy.redeemedTokens)
}
```

Add `"errors"` to the test file's imports if not already present (it is, per `internal/org/server.go`'s own imports — check `internal/org/server_test.go`'s current import block and add `"errors"` there too if missing).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/org/... -v`
Expected: FAIL — compile errors (`NewServer` arg count mismatch across all existing call sites, `InstancePolicy`/`fakeInstancePolicy.GetOrgCreationPolicy` undefined, `InviteToken` field undefined on `NetworkHabitatOrgCreateInput` if Task 1 wasn't completed first).

- [ ] **Step 3: Implement `InstancePolicy` and wire it into `CreateOrg`**

Read `internal/org/server.go` first. Add the interface and update the `Server` struct/`NewServer`:

```go
// InstancePolicy is the subset of instance-admin functionality org needs to
// enforce the instance's org-creation policy, without importing instanceadmin
// directly.
type InstancePolicy interface {
	GetOrgCreationPolicy(ctx context.Context) (string, error)
	RedeemInvite(ctx context.Context, token string) error
}

type Server struct {
	store          Store
	auth           authn.Method
	pear           pear.Pear
	domain         string
	decoder        *schema.Decoder
	dir            identity.Directory
	instancePolicy InstancePolicy
}

func NewServer(
	store Store,
	auth authn.Method,
	p pear.Pear,
	domain string,
	dir identity.Directory,
	instancePolicy InstancePolicy,
) (*Server, error) {
	return &Server{
		store:          store,
		auth:           auth,
		pear:           p,
		domain:         domain,
		decoder:        schema.NewDecoder(),
		dir:            dir,
		instancePolicy: instancePolicy,
	}, nil
}
```

In `CreateOrg`, insert this block after the existing field-validation checks (right before `orgID, id, err := s.store.CreateOrg(`):

```go
	policy, err := s.instancePolicy.GetOrgCreationPolicy(r.Context())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "checking org creation policy", http.StatusInternalServerError)
		return
	}
	if policy == "invite_only" {
		if req.InviteToken == "" {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				errors.New("an invite link is required to create an org on this instance"),
				"checking org creation policy",
				http.StatusForbidden,
			)
			return
		}
		if err := s.instancePolicy.RedeemInvite(r.Context(), req.InviteToken); err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				errors.New("an invite link is required to create an org on this instance"),
				"redeeming invite",
				http.StatusForbidden,
			)
			return
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/org/... -v`
Expected: PASS for all tests in the package.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/org/server.go
git add internal/org/server.go internal/org/server_test.go
git commit -m "$(cat <<'EOF'
Enforce instance org-creation policy in org.create

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Wire everything into `cmd/pear/main.go`

**Files:**
- Modify: `cmd/pear/main.go`

**Interfaces:**
- Consumes: `instanceadmin.NewStore(db *gorm.DB, domain string) (Store, error)` (Task 3), `instanceadmin.NewServer(store Store, frontendDomain string) *Server` (Task 4), `org.NewServer(..., instancePolicy InstancePolicy) (*Server, error)` (Task 5), `instanceadmin.Server.GetSettings/UpdateSettings/IssueInvite/DescribeInstance` (Task 4).

- [ ] **Step 1: Update the `instanceadmin.NewStore`/`NewServer` call sites**

Read `cmd/pear/main.go` first (the relevant lines are around 132 and 152). Change:

```go
	instanceAdminStore, err := instanceadmin.NewStore(db.WithContext(startupCtx))
```
to:
```go
	instanceAdminStore, err := instanceadmin.NewStore(db.WithContext(startupCtx), cmd.String(fDomain))
```

and change:
```go
	instanceAdminServer := instanceadmin.NewServer(instanceAdminStore)
```
to:
```go
	instanceAdminServer := instanceadmin.NewServer(instanceAdminStore, cmd.String(fFrontendDomain))
```

- [ ] **Step 2: Pass `instanceAdminStore` as the `InstancePolicy` to `org.NewServer`**

Find (around line 330):
```go
	orgServer, err := org.NewServer(orgStore, oauthServer, pear, domain, hiveDir)
```
Change to:
```go
	orgServer, err := org.NewServer(orgStore, oauthServer, pear, domain, hiveDir, instanceAdminStore)
```

(`instanceAdminStore` is `instanceadmin.Store`, which satisfies `org.InstancePolicy` structurally — no import of `org` needed in `instanceadmin`, and no new import needed here since `instanceAdminStore` is already in scope.)

- [ ] **Step 3: Register the new XRPC routes**

Find the existing instance-admin route block (around line 390):
```go
	mux.HandleFunc("/admin/login", instanceAdminServer.ServeLoginPage).Methods("GET")
	mux.HandleFunc("/admin/login", instanceAdminServer.HandleLogin).Methods("POST")
	mux.HandleFunc("/admin/logout", instanceAdminServer.HandleLogout).Methods("POST")
	mux.HandleFunc("/admin", instanceAdminServer.RequireSession(instanceAdminServer.ServeAdminHome)).
		Methods("GET")
```

Replace the last line (the page is no longer wrapped in `RequireSession` — `ServeAdminHome` now checks the session itself) and add the new routes:

```go
	mux.HandleFunc("/admin/login", instanceAdminServer.ServeLoginPage).Methods("GET")
	mux.HandleFunc("/admin/login", instanceAdminServer.HandleLogin).Methods("POST")
	mux.HandleFunc("/admin/logout", instanceAdminServer.HandleLogout).Methods("POST")
	mux.HandleFunc("/admin", instanceAdminServer.ServeAdminHome).Methods("GET")
	mux.HandleFunc("/xrpc/network.habitat.admin.getSettings", instanceAdminServer.GetSettings)
	mux.HandleFunc("/xrpc/network.habitat.admin.updateSettings", instanceAdminServer.UpdateSettings)
	mux.HandleFunc("/xrpc/network.habitat.admin.issueInvite", instanceAdminServer.IssueInvite)
	mux.HandleFunc("/xrpc/network.habitat.instance.describeInstance", instanceAdminServer.DescribeInstance)
```

- [ ] **Step 4: Build both modules**

Run: `go build ./...`
Expected: success.

Run: `cd cmd/pear && go build ./... && cd ../..`
Expected: success.

- [ ] **Step 5: Run the full Go test suite for touched packages**

Run: `go test ./internal/instanceadmin/... ./internal/org/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w cmd/pear/main.go
git add cmd/pear/main.go
git commit -m "$(cat <<'EOF'
Wire instance settings, invites, and describeInstance into pear's main

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Frontend — unauthenticated `query()` support + `describeInstance` wrapper

**Files:**
- Modify: `typescript/internal/src/habitatClient.ts`
- Create: `frontend/src/queries/instance.ts`

**Interfaces:**
- Consumes: generated TS type `NetworkHabitatInstanceDescribeInstance` (Task 1, under the `api` package).
- Produces (consumed by Task 8): `query()` callable with `{ unauthenticated: true, domain }` for query-type endpoints; `describeInstance(domain: string): Promise<NetworkHabitatInstanceDescribeInstance.OutputSchema>` exported from `frontend/src/queries/instance.ts`.

- [ ] **Step 1: Add the unauthenticated query type and import**

Read `typescript/internal/src/habitatClient.ts` first. Add `NetworkHabitatInstanceDescribeInstance` to the `from "api"` import block (the second multi-line `import type` block).

Add, right after the existing `type Query<...>` definition:

```ts
type UnauthedQuery<
  Params extends Record<string, string | number | boolean | string[]>,
  Output,
> = {
  params: Params;
  output: Output;
  unauthenticated: true;
};
```

Add the new endpoint to `QueryEndpoints`:

```ts
  "network.habitat.instance.describeInstance": UnauthedQuery<
    NetworkHabitatInstanceDescribeInstance.QueryParams,
    NetworkHabitatInstanceDescribeInstance.OutputSchema
  >;
```

- [ ] **Step 2: Make `query()`'s options type switch on `unauthenticated`, like `procedure()` already does**

Find:
```ts
export const query = async <T extends keyof QueryEndpoints>(
  endpoint: T,
  params: QueryEndpoints[T]["params"],
  options: AuthedOptions,
): Promise<QueryEndpoints[T]["output"]> => {
```

Add this type alias right before it (mirroring `ProcedureOptions`):

```ts
type QueryOptions<T extends keyof QueryEndpoints> =
  QueryEndpoints[T]["unauthenticated"] extends true
    ? UnauthedOptions
    : AuthedOptions;
```

Change the signature's `options` parameter type to `QueryOptions<T>`, and change the body to branch on `options.unauthenticated`:

```ts
export const query = async <T extends keyof QueryEndpoints>(
  endpoint: T,
  params: QueryEndpoints[T]["params"],
  options: QueryOptions<T>,
): Promise<QueryEndpoints[T]["output"]> => {
  const queryParams = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue;
    if (Array.isArray(value)) {
      for (const v of value) {
        queryParams.append(key, v.toString());
      }
    } else {
      queryParams.set(key, value.toString());
    }
  }
  const path = "/xrpc/" + endpoint + "?" + queryParams.toString();
  const response = options.unauthenticated
    ? await fetch(`https://${(options as UnauthedOptions).domain}${path}`)
    : await (options as AuthedOptions).authManager.fetch(
        path,
        "GET",
        null,
        options.headers,
        (options as AuthedOptions).fetchOptions,
      );
  try {
    const data = await response.json();
    if (!response.ok) {
      throw new XRPCError(response.status, data);
    }
    return data;
  } catch {
    throw new Error(`Invalid error response: ${response.status}`);
  }
};
```

- [ ] **Step 3: Add the `describeInstance` query wrapper**

Read `frontend/src/queries/org.ts` first for the existing style (not strictly required to modify it, just to match conventions). Create `frontend/src/queries/instance.ts`:

```ts
import { query } from "internal";
import type { NetworkHabitatInstanceDescribeInstance } from "api";

export function describeInstance(
  domain: string,
): Promise<NetworkHabitatInstanceDescribeInstance.OutputSchema> {
  return query(
    "network.habitat.instance.describeInstance",
    {},
    { unauthenticated: true, domain },
  );
}
```

- [ ] **Step 4: Type-check the frontend**

Run: `moon frontend:typecheck`
Expected: succeeds with no type errors. If `query` exports from `internal` need re-exporting (check `typescript/internal/src/index.ts` for an existing `export * from "./habitatClient"` or named export of `query`/`procedure` — they're already used as `import { procedure, query } from "internal"` elsewhere, so no change should be needed here).

- [ ] **Step 5: Commit**

```bash
git add typescript/internal/src/habitatClient.ts frontend/src/queries/instance.ts
git commit -m "$(cat <<'EOF'
Support unauthenticated query() calls and add describeInstance wrapper

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Frontend — `org/create` page picks up invite tokens and instance selection

**Files:**
- Modify: `frontend/src/routes/org/create.tsx`

**Interfaces:**
- Consumes: `describeInstance(domain)` (Task 7), `NetworkHabitatOrgCreate.InputSchema.invite_token` (Task 1).

- [ ] **Step 1: Read the current file**

Read `frontend/src/routes/org/create.tsx` (already read during design — re-read here to confirm no other changes have landed since).

- [ ] **Step 2: Add search param validation, token decoding, and the instance picker**

Replace the file's route definition and the top of `CreateOrgPage` (everything from the imports through the `onSubmit` function) with:

```tsx
import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  ToggleGroup,
  ToggleGroupItem,
} from "internal/components/ui";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Controller, useForm } from "react-hook-form";
import { procedure, SingleHandleCombobox } from "internal";
import { describeInstance } from "../../queries/instance";
import { NetworkHabitatOrgCreate } from "api";
import { useEffect, useState } from "react";

export const Route = createFileRoute("/org/create")({
  validateSearch: (search: Record<string, unknown>) => ({
    token: search.token ? String(search.token) : undefined,
  }),
  component: CreateOrgPage,
});

interface FormValues {
  name: string;
  admin_handle: string;
  admin_password: string;
  login_method: "password" | "atproto" | "google";
  login_id: string;
  handle_subdomain: string;
}

interface DecodedInvite {
  domain: string;
  name: string;
}

function decodeInviteToken(token: string): DecodedInvite | null {
  try {
    const payload = token.split(".")[1];
    const json = atob(payload.replace(/-/g, "+").replace(/_/g, "/"));
    const claims = JSON.parse(json);
    if (typeof claims.domain !== "string" || typeof claims.name !== "string") {
      return null;
    }
    return { domain: claims.domain, name: claims.name };
  } catch {
    return null;
  }
}

function PasswordInput(props: React.ComponentProps<typeof Input>) {
  const [isPlain, setIsPlain] = useState(true);

  return (
    <Input
      type={isPlain ? "text" : "password"}
      {...props}
      onFocus={() => setIsPlain(false)}
    />
  );
}

function CreateOrgPage() {
  const navigate = useNavigate();
  const { token } = Route.useSearch();
  const invite = token ? decodeInviteToken(token) : null;

  const [useCustomInstance, setUseCustomInstance] = useState(false);
  const [customDomain, setCustomDomain] = useState("");
  const [customInstanceName, setCustomInstanceName] = useState<string | null>(
    null,
  );
  const [customInstanceError, setCustomInstanceError] = useState<
    string | null
  >(null);

  useEffect(() => {
    if (invite || !useCustomInstance || !customDomain) {
      setCustomInstanceName(null);
      setCustomInstanceError(null);
      return;
    }
    let cancelled = false;
    describeInstance(customDomain)
      .then((result) => {
        if (!cancelled) {
          setCustomInstanceName(result.name);
          setCustomInstanceError(null);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setCustomInstanceName(null);
          setCustomInstanceError("Could not reach that instance.");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [invite, useCustomInstance, customDomain]);

  const targetDomain = invite ? invite.domain : useCustomInstance
    ? customDomain
    : __HABITAT_DOMAIN__;

  const {
    register,
    handleSubmit,
    setError,
    setValue,
    watch,
    formState: { isSubmitting, errors },
    control,
  } = useForm<FormValues>({
    defaultValues: {
      admin_handle: "admin",
      admin_password: "",
      handle_subdomain: "acmecorp",
      name: "My Organization",
      login_method: "password",
      login_id: "",
    },
  });

  const loginMethod = watch("login_method");

  const onSubmit = async (values: FormValues) => {
    try {
      let body: NetworkHabitatOrgCreate.InputSchema = {
        admin_handle: values.admin_handle,
        name: values.name || undefined,
        login_method: values.login_method,
        handle_subdomain: values.handle_subdomain,
        invite_token: token,
      };
      if (values.login_method === "password") {
        body.admin_password = values.admin_password;
      } else {
        body.login_id = values.login_id || undefined;
      }
      const { admin_handle } = await procedure(
        "network.habitat.org.create",
        body,
        { unauthenticated: true, domain: targetDomain },
      );
      await navigate({
        to: "/oauth-login",
        search: { handle: admin_handle },
      });
    } catch (err) {
      setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    }
  };
```

- [ ] **Step 3: Render the instance-selection / read-only-instance-name UI**

In the returned JSX, right after the `<h1>Create Organization</h1>` line, add:

```tsx
      {invite ? (
        <Field>
          <FieldLabel>Hosted on</FieldLabel>
          <Input value={invite.name} disabled />
        </Field>
      ) : (
        <Field>
          <FieldLabel>Hosting</FieldLabel>
          <ToggleGroup
            variant="outline"
            value={[useCustomInstance ? "custom" : "managed"]}
            onValueChange={(v) => setUseCustomInstance(v[0] === "custom")}
          >
            <ToggleGroupItem value="managed">
              Managed hosting by Habitat
            </ToggleGroupItem>
            <ToggleGroupItem value="custom">Custom instance</ToggleGroupItem>
          </ToggleGroup>
          {useCustomInstance ? (
            <>
              <Input
                placeholder="myinstance.example.com"
                value={customDomain}
                onChange={(e) => setCustomDomain(e.target.value)}
              />
              {customInstanceName ? (
                <Input value={customInstanceName} disabled />
              ) : null}
              {customInstanceError ? (
                <p className="text-sm text-destructive">
                  {customInstanceError}
                </p>
              ) : null}
            </>
          ) : null}
        </Field>
      )}
```

This goes inside the existing `<fieldset disabled={isSubmitting} className="flex flex-col gap-4">`, before the `Organization Name` field.

- [ ] **Step 4: Type-check and build the frontend**

Run: `moon frontend:typecheck`
Expected: succeeds with no type errors.

Run: `moon frontend:build`
Expected: succeeds.

- [ ] **Step 5: Manual verification**

Run: `moon :dev-all` (or `moon pear:dev` + `moon frontend:dev` separately), then:
1. Visit `/admin`, log in with the bootstrap password, set policy to invite-only with a manager name "Acme Hosting", click "Generate invite link", copy the shown URL.
2. Open that URL in the browser → confirm the "Hosted on" field shows "Acme Hosting" (read-only) and submitting creates the org successfully.
3. Visit `/org/create` with no token → confirm "Managed hosting by Habitat" is selected by default and org creation succeeds with no invite token.
4. On `/org/create`, switch to "Custom instance" and type the same instance's domain → confirm the read-only name field populates via `describeInstance`, and submitting without a token (since this instance is invite-only) surfaces the 403 error message in the form.
5. Set the instance back to "open" policy in `/admin` → confirm `/org/create` (no token, managed hosting) succeeds again.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/routes/org/create.tsx
git commit -m "$(cat <<'EOF'
Support invite-token and custom-instance selection in org creation UI

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```
