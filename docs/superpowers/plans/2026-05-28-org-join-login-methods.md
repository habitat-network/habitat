# Org Join Login Methods Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the `/org/join` page to show login-method-appropriate UI (password/atproto/google) instead of always showing a password form.

**Architecture:** Backend changes update the `getMetadata` output with `loginMethod` and `handleSubdomain`, make `getMetadata` publicly accessible by `orgId`, and update `CreateNewMemberIdentity` to dispatch by login method. The frontend rewrites `/org/join` to fetch metadata and render the right auth UI.

**Tech Stack:** Go (org package), React/TanStack Router (frontend), lexicon JSON files

---

### Task 1: Update getMetadata lexicon and generated types

**Files:**
- Modify: `lexicons/network/habitat/org/getMetadata.json`

- [ ] **Step 1: Add `loginMethod` and `handleSubdomain` to the getMetadata lexicon output**

Edit `lexicons/network/habitat/org/getMetadata.json` — add `loginMethod` and `handleSubdomain` to properties:

```json
{
    "lexicon": 1,
    "id": "network.habitat.org.getMetadata",
    "defs": {
        "main": {
            "type": "query",
            "description": "Get general info about this organization.",
            "output": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": [
                        "domain",
                        "loginMethod",
                        "handleSubdomain"
                    ],
                    "properties": {
                        "domain": {
                            "type": "string",
                            "description": "The domain where habitat is hosted for this organization."
                        },
                        "name": {
                            "type": "string",
                            "description": "The name of this organization."
                        },
                        "description": {
                            "type": "string",
                            "description": "A description for this organization."
                        },
                        "loginMethod": {
                            "type": "string",
                            "description": "Login method for the org: 'password', 'atproto', or 'google'."
                        },
                        "handleSubdomain": {
                            "type": "string",
                            "description": "The subdomain used for all org member handles."
                        }
                    }
                }
            }
        }
    }
}
```

- [ ] **Step 2: Regenerate types from lexicons**

Run: `moon :generate`

This regenerates `api/habitat/org_getMetadata.go` and `typescript/api/types/network/habitat/org/getMetadata.ts`.

- [ ] **Step 3: Commit**

```bash
git add lexicons/network/habitat/org/getMetadata.json api/habitat/org_getMetadata.go typescript/api/types/network/habitat/org/getMetadata.ts
git commit -m "feat: add loginMethod and handleSubdomain to getMetadata output"
```

---

### Task 2: Update `getMetadata` implementation on `orgImpl`

**Files:**
- Modify: `internal/org/org.go` (orgImpl.GetMetadata, lines 114-123)

- [ ] **Step 1: Populate loginMethod and handleSubdomain in the returned output**

Replace the method body:

```go
func (s *orgImpl) GetMetadata(ctx context.Context, domain string) habitat.NetworkHabitatOrgGetMetadataOutput {
	var org organization
	if err := s.db.WithContext(ctx).First(&org, "id = ?", s.orgID).Error; err != nil {
		return habitat.NetworkHabitatOrgGetMetadataOutput{}
	}
	return habitat.NetworkHabitatOrgGetMetadataOutput{
		Domain:          domain,
		Name:            org.Name,
		LoginMethod:     string(org.LoginMethod),
		HandleSubdomain: org.HandleSubdomain,
	}
}
```

The `LoginMethod` and `HandleSubdomain` fields now exist on `NetworkHabitatOrgGetMetadataOutput` from the regenerated types in Task 1.

- [ ] **Step 2: Commit**

```bash
git add internal/org/org.go
git commit -m "feat: populate loginMethod and handleSubdomain in getMetadata"
```

---

### Task 3: Make getMetadata publicly accessible by orgId

**Files:**
- Modify: `internal/org/server.go` (add `GetPublicMetadata` handler)
- Modify: `cmd/pear/main.go` (register the route)

- [ ] **Step 1: Add `GetPublicMetadata` handler to `internal/org/server.go`**

Add this new handler (no auth check — reads `orgId` from query params):

```go
// GetPublicMetadata returns org metadata by org ID. No auth required.
// Used by the join page to show org info before the user joins.
func (s *Server) GetPublicMetadata(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("orgId")
	if orgID == "" {
		utils.LogAndHTTPError(w, nil, "missing orgId query parameter", http.StatusBadRequest)
		return
	}

	org, err := s.store.GetOrg(r.Context(), orgID)
	if err != nil {
		utils.LogAndHTTPError(w, err, "organization not found", http.StatusNotFound)
		return
	}

	meta := org.GetMetadata(r.Context(), s.domain)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
	}
}
```

- [ ] **Step 2: Register the new route in `cmd/pear/main.go`**

Find the route registration block (around line 283) and point the `network.habitat.org.getMetadata` XRPC path to the new public handler:

```go
mux.HandleFunc("/xrpc/network.habitat.org.getMetadata", orgServer.GetPublicMetadata)
```

- [ ] **Step 3: Commit**

```bash
git add internal/org/server.go cmd/pear/main.go
git commit -m "feat: add public getMetadata endpoint for join page"
```

---

### Task 4: Update mintMemberIdentity lexicon and generated types

**Files:**
- Modify: `lexicons/network/habitat/org/mintMemberIdentity.json`

- [ ] **Step 1: Make `password` optional, add `loginID` field**

Edit `lexicons/network/habitat/org/mintMemberIdentity.json`. Remove `password` from `required`, add `loginID` as an optional property:

```json
{
    "lexicon": 1,
    "id": "network.habitat.org.mintMemberIdentity",
    "defs": {
        "main": {
            "type": "procedure",
            "description": "Mint a new organization member identity with the given handle and token.",
            "input": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": [
                        "token",
                        "handle"
                    ],
                    "properties": {
                        "orgId": {
                            "type": "string",
                            "description": "The ID of the org this member is joining."
                        },
                        "handle": {
                            "type": "string",
                            "description": "The internal handle (all letters + numbers, no special characters, does not include org domain) that will be used by the member."
                        },
                        "token": {
                            "type": "string",
                            "description": "The token that was issued by an org admin to allow members to join the organization."
                        },
                        "password": {
                            "type": "string",
                            "description": "The password for the new member's account (required for 'password' login method)."
                        },
                        "loginID": {
                            "type": "string",
                            "description": "Provider-specific identifier (AT Protocol handle for 'atproto', email for 'google'). Required for non-password login methods."
                        }
                    }
                }
            },
            "output": {
                "encoding": "application/json",
                "schema": {
                    "type": "object",
                    "required": [
                        "handle",
                        "did"
                    ],
                    "properties": {
                        "handle": {
                            "type": "string",
                            "description": "The full handle of the newly minted member identity."
                        },
                        "did": {
                            "type": "string",
                            "description": "The DID of the newly minted member identity."
                        }
                    }
                }
            }
        }
    }
}
```

- [ ] **Step 2: Regenerate types**

Run: `moon :generate`

- [ ] **Step 3: Commit**

```bash
git add lexicons/network/habitat/org/mintMemberIdentity.json api/habitat/org_mintMemberIdentity.go typescript/api/types/network/habitat/org/mintMemberIdentity.ts
git commit -m "feat: make password optional, add loginID to mintMemberIdentity"
```

---

### Task 5: Update `CreateNewMemberIdentity` to dispatch by login method

**Files:**
- Modify: `internal/org/org.go` (interface + implementation)
- Modify: `internal/org/public.go` (everyoneOrg stub)

- [ ] **Step 1: Update `Org` interface — add `loginID` parameter**

Change lines 72-77 of `internal/org/org.go`:

```go
CreateNewMemberIdentity(
    ctx context.Context,
    token string,
    internalHandle string,
    password string,
    loginID string,
) (*identity.Identity, error)
```

- [ ] **Step 2: Rewrite `orgImpl.CreateNewMemberIdentity`**

Replace the implementation (lines 323-354) with login-method-aware dispatch:

```go
func (s *orgImpl) CreateNewMemberIdentity(
	ctx context.Context,
	token string,
	internalHandle string,
	password string,
	loginID string,
) (*identity.Identity, error) {
	if err := s.validateIdentityToken(ctx, token); err != nil {
		return nil, err
	}

	var memberLoginID string
	switch s.LoginMethod(ctx) {
	case LoginMethodPassword:
		if password == "" {
			return nil, errors.New("password is required for password login method")
		}
		hash, err := hashPassword(password)
		if err != nil {
			return nil, err
		}
		memberLoginID = hash
	case LoginMethodAtproto, LoginMethodGoogle:
		if loginID == "" {
			return nil, fmt.Errorf("loginID is required for %s login method", s.LoginMethod(ctx))
		}
		memberLoginID = loginID
	default:
		return nil, fmt.Errorf("unknown login method: %s", s.LoginMethod(ctx))
	}

	var id *identity.Identity
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		newId, err := s.hive.WithTx(tx).MintIdentity(internalHandle, s.handleSubdomain)
		if err != nil {
			return fmt.Errorf("mint identity: %w", err)
		}
		id = newId
		return s.addMemberTx(ctx, tx, id.DID, memberLoginID)
	})
	if err != nil {
		return nil, err
	}
	return id, nil
}
```

- [ ] **Step 3: Update `everyoneOrg.CreateNewMemberIdentity` in `public.go`**

Match the new signature:

```go
func (e *everyoneOrg) CreateNewMemberIdentity(
	ctx context.Context,
	token string,
	internalHandle string,
	password string,
	loginID string,
) (*identity.Identity, error) {
	return nil, ErrNotSupportedPublic
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/org/org.go internal/org/public.go
git commit -m "feat: dispatch CreateNewMemberIdentity by login method"
```

---

### Task 6: Update `MintMemberIdentity` server handler

**Files:**
- Modify: `internal/org/server.go` (MintMemberIdentity, lines 407-466)

- [ ] **Step 1: Relax validation, pass loginID through**

Replace the strict validation (line 415) that requires `password`:

```go
if req.Token == "" || req.Handle == "" || req.OrgId == "" {
    w.WriteHeader(http.StatusBadRequest)
    return
}
```

Then change the `CreateNewMemberIdentity` call (line 426) to pass the `loginID`:

```go
id, err := org.CreateNewMemberIdentity(r.Context(), req.Token, req.Handle, req.Password, req.LoginID)
```

- [ ] **Step 2: Commit**

```bash
git add internal/org/server.go
git commit -m "feat: pass loginID through MintMemberIdentity handler"
```

---

### Task 7: Rewrite `/org/join` frontend page

**Files:**
- Rewrite: `frontend/src/routes/org/join.tsx`
- Modify: `frontend/src/queries/org.ts`

- [ ] **Step 1: Add public `getOrgMetadataQueryOptions` to `queries/org.ts`**

```typescript
export function getOrgMetadataQueryOptions(orgId: string) {
  return queryOptions({
    queryKey: ["orgMetadata", orgId],
    queryFn: () =>
      query("network.habitat.org.getMetadata", { orgId }, { unauthenticated: true }),
    staleTime: Infinity,
  });
}
```

- [ ] **Step 2: Rewrite `frontend/src/routes/org/join.tsx`**

Full replacement:

```tsx
import {
  Button,
  Field,
  FieldError,
  FieldLabel,
  Input,
  Combobox,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "internal/components/ui";
import { createFileRoute } from "@tanstack/react-router";
import { useForm, Controller } from "react-hook-form";
import { useState, useEffect, useRef } from "react";
import { procedure, searchActorsTypeahead, UserAvatar } from "internal";
import { useQuery } from "@tanstack/react-query";
import type { Actor } from "internal";
import { getOrgMetadataQueryOptions } from "@/queries/org";

export const Route = createFileRoute("/org/join")({
  validateSearch: (search: Record<string, unknown>) => ({
    token: String(search.token ?? ""),
    orgId: String(search.orgId ?? ""),
  }),
  component: JoinPage,
});

type FormValues = {
  handle: string;
  password: string;
  loginID: string;
};

function useDebounce<T>(value: T, delay: number): T {
  const [debouncedValue, setDebouncedValue] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedValue(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debouncedValue;
}

function HandleCombobox({
  value,
  onValueChange,
}: {
  value: string;
  onValueChange: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [searchValue, setSearchValue] = useState(value || "");
  const debouncedSearchValue = useDebounce(searchValue, 250);
  const inputRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setSearchValue(value || "");
  }, [value]);

  const { data: suggestions = [] } = useQuery<Actor[]>({
    queryKey: ["actorSearch", debouncedSearchValue],
    queryFn: () => searchActorsTypeahead(debouncedSearchValue),
    enabled: !!debouncedSearchValue.trim(),
  });

  return (
    <Combobox
      items={suggestions}
      open={open}
      onOpenChange={setOpen}
      onValueChange={(actor: Actor | null) => {
        if (actor?.handle) {
          onValueChange(actor.handle);
          setSearchValue(actor.handle);
          setOpen(false);
        }
      }}
    >
      <div ref={inputRef}>
        <Input
          placeholder="alice.bsky.social"
          value={searchValue}
          onChange={(e) => {
            setSearchValue(e.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
        />
      </div>
      <ComboboxContent anchor={inputRef}>
        <ComboboxEmpty>No results found.</ComboboxEmpty>
        <ComboboxList>
          {(item: Actor) => (
            <ComboboxItem key={item.handle} value={item}>
              <UserAvatar actor={item} size="sm" />
              {item.displayName || item.handle}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

function JoinPage() {
  const { token, orgId } = Route.useSearch();
  const [result, setResult] = useState<{ handle: string; did: string } | null>(null);

  const { data: metadata, isLoading: metaLoading } = useQuery(
    getOrgMetadataQueryOptions(orgId),
  );

  const {
    register,
    handleSubmit,
    setError,
    control,
    formState: { isSubmitting, errors },
  } = useForm<FormValues>();

  const onSubmit = async (values: FormValues) => {
    try {
      const body: Record<string, string> = {
        token,
        orgId,
        handle: values.handle,
      };
      if (metadata?.loginMethod === "password") {
        body.password = values.password;
      } else {
        body.loginID = values.loginID;
      }
      const res = await procedure(
        "network.habitat.org.mintMemberIdentity",
        body,
        { unauthenticated: true },
      );
      setResult({ handle: res.handle, did: res.did });
    } catch (err) {
      setError("root", {
        message: err instanceof Error ? err.message : "Unknown error",
      });
    }
  };

  if (result) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <h1 className="text-2xl font-semibold">Welcome!</h1>
        <p className="text-muted-foreground">Your account has been created.</p>
        <div className="flex flex-col gap-1 text-sm font-mono">
          <span>{result.handle}</span>
          <span className="text-muted-foreground">{result.did}</span>
        </div>
      </div>
    );
  }

  if (metaLoading) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <p className="text-muted-foreground">Loading...</p>
      </div>
    );
  }

  if (!metadata) {
    return (
      <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
        <p className="text-muted-foreground">Organization not found.</p>
      </div>
    );
  }

  const loginMethod = metadata.loginMethod;
  const handleSubdomain = metadata.handleSubdomain;

  return (
    <div className="flex flex-col gap-4 max-w-md mx-auto mt-16">
      <h1 className="text-2xl font-semibold">
        Join {metadata.name || handleSubdomain} Organization
      </h1>
      <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
        <Field>
          <FieldLabel>
            Handle
            {loginMethod === "password" ? (
              <span className="text-gray-400 text-sm ml-1 font-normal">
                This will look like your-handle.{handleSubdomain}
              </span>
            ) : null}
          </FieldLabel>
          <Input
            placeholder="handle"
            disabled={isSubmitting}
            {...register("handle", { required: true })}
          />
          <FieldError errors={[errors.handle]} />
        </Field>
        {loginMethod === "password" ? (
          <Field>
            <FieldLabel>Password</FieldLabel>
            <Input
              type="password"
              placeholder="password"
              disabled={isSubmitting}
              {...register("password", { required: true })}
            />
            <FieldError errors={[errors.password]} />
          </Field>
        ) : (
          <Field>
            <FieldLabel>
              {loginMethod === "atproto" ? "AT Protocol Handle" : "Google Email"}
            </FieldLabel>
            <Controller
              control={control}
              name="loginID"
              rules={{ required: true }}
              render={({ field: { onChange, value } }) =>
                loginMethod === "atproto" ? (
                  <HandleCombobox value={value ?? ""} onValueChange={onChange} />
                ) : (
                  <Input
                    placeholder="user@gmail.com"
                    value={value ?? ""}
                    onChange={(e) => onChange(e.target.value)}
                  />
                )
              }
            />
            <FieldError errors={[errors.loginID]} />
          </Field>
        )}
        <FieldError errors={[errors.root]} />
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? "Joining..." : "Join"}
        </Button>
      </form>
    </div>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/queries/org.ts frontend/src/routes/org/join.tsx
git commit -m "feat: rewrite org join page with login-method-aware UI"
```

---

### Task 8: Update invite link to include orgId

**Files:**
- Modify: `frontend/src/routes/_requireAuth/org/index.tsx` (InviteSection, line 76)

The `/org/join` route now requires `orgId` in the URL. The admin-facing invite section needs to include it when generating the invite link.

- [ ] **Step 1: Get orgId from the route context or root loader data**

In the `_requireAuth/org/index.tsx` route, access the root route's loader data to get the org metadata (already fetched in `__root.tsx`). Add a `orgId` field to the route's returned loader data by fetching it from the root.

The root route (`__root.tsx`) already fetches `getConfigQueryOptions` which returns `{ domain, name, description, loginMethod, handleSubdomain }`. We need `orgId` added to the output too. BUT: since that requires an additional lexicon change, we can instead fetch it directly in the org page's loader.

Add a `getOrgId` query to the org page's loader, or simply lookup the org ID from the metadata response. Since the original `getMetadata` handler (the authenticated one) already knows which org the caller belongs to, we can add `orgId` to its output.

For minimal friction: add `orgId` to the `getMetadata` lexicon output (same pattern as Task 1), regenerate, and populate it in `orgImpl.GetMetadata`. Then the root loader's returned `org` object will contain `orgId`, and the org page can access it.

**Sub-steps:**

1. Add `orgId` to the getMetadata lexicon output properties:

```json
"orgId": {
    "type": "string",
    "description": "The unique ID of this organization."
}
```

Add `"orgId"` to the `required` array. Run `moon :generate`.

2. Populate `orgId` in `orgImpl.GetMetadata`:

```go
OrgId: org.ID,
```

3. In the org route (`_requireAuth/org/index.tsx`), access the root loader data via TanStack Router's context to get the `orgId`. Pass it into the `InviteSection` component and include it in the invite URL:

```tsx
// In OrgPage, use root route loader data
const rootData = Route.useLoaderData({ from: "__root__" });
const orgId = rootData?.org?.orgId;

// Then in JSX:
<InviteSection authManager={authManager} orgId={orgId} />
```

4. Update `InviteSection` to accept `orgId` prop and include it:

```tsx
function InviteSection({
  authManager,
  orgId,
}: {
  authManager: Parameters<typeof issueInviteToken>[0];
  orgId: string;
}) {
  // ...
  const { mutate: generateLink, isPending } = useMutation({
    mutationFn: () => issueInviteToken(authManager),
    onSuccess: ({ token }) => {
      setInviteUrl(`${window.location.origin}/org/join?token=${token}&orgId=${orgId}`);
    },
  });
  // ...
}
```

- [ ] **Step 2: Commit**

```bash
git add lexicons/network/habitat/org/getMetadata.json internal/org/org.go api/habitat/org_getMetadata.go frontend/src/routes/_requireAuth/org/index.tsx typescript/api/types/network/habitat/org/getMetadata.ts
git commit -m "feat: add orgId to metadata, include in invite link"
```

---

### Spec Self-Review Checklist

- **Spec coverage check:** Does each requirement have a corresponding task?
  - getMetadata with loginMethod + handleSubdomain → Tasks 1, 2
  - getMetadata public by orgId → Task 3
  - mintMemberIdentity with optional password + loginID → Task 4
  - CreateNewMemberIdentity dispatching by login method → Task 5
  - MintMemberIdentity handler updated → Task 6
  - /org/join rewrite → Task 7
  - Invite link with orgId → Task 8
  All covered.

- **Placeholder scan:** All tasks contain concrete code and commands. No TBD/TODO/fill-in-later.

- **Type consistency:** `NetworkHabitatOrgGetMetadataOutput` gains `LoginMethod`, `HandleSubdomain`, and `OrgId` fields from the regenerated code. The `CreateNewMemberIdentity` signature change is consistent across interface, impl, and stub.

- **Scope check:** This is a single focused feature (join page login methods). It touches both Go backend and React frontend but they form a single deployable unit.
