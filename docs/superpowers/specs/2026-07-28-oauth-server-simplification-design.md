# OAuth Server Simplification — DB-Backed Persistence

## Motivation

The OAuth server uses a mix of strategies to avoid server-side state: stateless encrypted-CBOR authorization codes, encrypted cookies for in-flight flow state, and in-memory stores for PAR sessions and client assertion JWT dedup. This makes the code harder to follow and maintain than necessary. By using the database for all OAuth state, we can:

- Remove the custom authorize code strategy (encrypt/decrypt CBOR in the code)
- Remove in-memory stores
- Use Fosite's standard HMAC strategy for codes
- Slim down the encrypted cookie to only the PDS provider state (one field, one redirect hop)

## Data Model

### New table: `oauth_requests`

A single unified table for all OAuth ephemeral state — PAR sessions, pending authorization flows (replacing the cookie), authorize code sessions (used by fosite's HMAC strategy), and client assertion JWT dedup.

```go
type OAuthRequest struct {
    Key                string     `gorm:"primaryKey"`          // PAR request_uri / PDS state param / HMAC signature / JTI
    ClientID           string     `gorm:"size:1024"`
    Subject            string     `gorm:"size:255"`            // DID (populated after handle resolution)
    Scopes             string     `gorm:"size:512"`            // requested scopes
    CodeChallenge      string     `gorm:"size:255"`
    CodeChallengeMethod string    `gorm:"size:32"`
    RedirectURI        string     `gorm:"size:1024"`
    State              string     `gorm:"size:1024"`           // client's OAuth state param (echoed back in final redirect)
    ResponseType       string     `gorm:"size:64"`
    ExpiresAt          time.Time  `gorm:"index"`
}
```

### Existing tables (unchanged)

- `oauth_sessions` — refresh token sessions (Signature PK, ClientID, Subject, Scopes, ExpiresAt)
- `connected_apps` — app authorization records (Subject + ClientID composite PK, Scopes, CreatedAt, UpdatedAt)

### OAuthSession note

`OAuthSession` uses a `Signature` column as PK (the hashed refresh token). This table is already clean and unchanged. The garbage collector cleans expired rows here too.

## Authorize Flow (replacing cookies)

**Before:**
1. `NewAuthorizeRequest` → processes PAR (creates authorizer from PAR session)
2. Resolve handle → DID
3. `loginRouter.Authorize(did)` → `providerState` + `redirectURL`
4. Store `authRequestFlash{Form, ProviderState, DID}` in encrypted cookie
5. Redirect user to PDS `redirectURL`
6. Callback reads cookie, calls `provider.NewAuthorizeResponse`

**After:**
1. Call `GetPARSession(requesterURI)` directly — skips `NewAuthorizeRequest`'s delete
2. Resolve handle → DID
3. `loginRouter.Authorize(did)` → `providerState` + `redirectURL`
4. Update the requester's fields (set `Subject` to DID) in the store under a new key: the `state` param we send to the PDS
   - Provider state is stored in a small encrypted cookie (one field, single redirect hop)
5. Redirect user to PDS with that `state` param
6. Callback reads the small cookie for `providerState`, calls `GetPARSession(state)` to get the requester
7. `loginRouter.Exchange(did, callbackQuery, providerState)` → PDS tokens
8. `provider.NewAuthorizeResponse(ctx, requester)` → generates auth code
9. `DeletePARSession(state)` — cleanup

## HMAC Strategy (replaces custom encrypted-CBOR codes)

Remove `fosite_strategy.go`. Use `oauth2.NewDefaultJWTStrategy(config, globalSecret)` — it already uses HMAC for authorize codes (based on `config.GlobalSecret`) and JWT for access/refresh tokens. No custom overrides needed.

## Garbage Collector

A goroutine started from `cmd/pear/main.go`:

```go
type Collector struct {
    db       *gorm.DB
    interval time.Duration
}

func (gc *Collector) Run(ctx context.Context) {
    ticker := time.NewTicker(gc.interval)
    for {
        select {
        case <-ticker.C:
            gc.db.Where("expires_at < ?", time.Now()).Delete(&OAuthRequest{})
            gc.db.Where("expires_at < ?", time.Now()).Delete(&OAuthSession{})
        case <-ctx.Done():
            ticker.Stop()
            return
        }
    }
}
```

## Files Changed

| File | Change |
|------|--------|
| `internal/oauthserver/fosite_storage.go` | Rewrite: remove `storage.MemoryStore`, implement all storage methods via `OAuthRequest` table, add `AutoMigrate`, remove session serialization helpers |
| `internal/oauthserver/fosite_strategy.go` | Delete |
| `internal/oauthserver/fosite_session.go` | Remove `getAuthCode`/`decodeSession`, simplify to plain data struct with DID, scopes, client ID, expiry |
| `internal/oauthserver/oauth_server.go` | Slim down cookie to only provider state; rewrite flow to use `GetPARSession` directly + re-store under state key |
| `internal/oauthserver/oauth_server_test.go` | Update tests for new flow |
| `cmd/pear/main.go` | Wire up garbage collector |

## Dependencies removed

- Custom CBOR encryption/decryption from `fosite_session.go`
- In-memory fosite storage (`storage.MemoryStore`)
