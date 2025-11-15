# Habitat OAuth Server/Provider

Habitat uses a user's atproto PDS as an identity provider to provide a brokered OAuth server to client apps.

### Terminology

1. **User**: the user of the App that has data and permissions stored in Habitat and an identity stored in their PDS.
1. **App**: the app that wants to use Habitat to access user data. It is the "client" in the Habitat OAuth Flow
1. **Habitat**: the Habitat server that authenticates users and authorizes apps. It is the "server" in the Habitat OAuth Flow and the "client" in the PDS OAuth Flow
1. **Habitat OAuth Flow**: the authentication flow between App and Habitat
1. **Habitat Token**: the information needed by the App to make authenticated requests to Habitat
1. **PDS**: the user's atproto PDS that is pointed to by their DID document. It is the "server" in the PDS OAuth flow
1. **PDS OAuth Flow**: the authentication flow between Habitat and Atproto
1. **PDS Token**: the information needed by Habitat to make authenticated requests to the PDS (access token + refresh token)

## Authentication Flow

### 1. App issues an `/authorize` request beginning Habitat OAuth Flow

App navigates the user to Habitat's `/authorize` endpoint.
The request's query includes the user's handle along with standard OAuth authorize parameters.
Habitat parses the request and will stop the flow by responding with any errors.

### 2. Habitat begins PDS OAuth Flow

If the request is valid, Habtiat will initiate the PDS OAuth Flow.
It will resolves the handle and create a DPop key needed for the PDS OAuth Flow.
It calls `auth.OAuthClient.Authorize` which returns a url where the User can enter their credentials.
The `/authorize` response redirects the User to this url.
Before redirecting, Habitat saves what it needs to continue both the Habitat OAuth Flow and the PDS OAuth Flow in the response cookie.
This includes the `/authorize` params, the DPop key, and the PDS OAuth Flow state.

### 3. User authenticates with PDS

The user enters their credentials and the PDS redirects to Habitat's `/callback` endpoint which was encoded in the redirect url.
The `/callback` request includes information needed to complete the PDS OAuth flow (authorization code).

### 4. Habitat completes the PDS OAuth Flow

Habitat retrieves the `/authorize` params, the DPop key, and the PDS OAuth Flow state from the request cookie.
Habitat calls auth.OAuthClient.ExchangeCode with the necessary arguments (DPop key, PDS OAuth Flow state, and authorization code) to retrieve the PDS Token.
Using the `/authorize` params, it continues the Habitat OAuth Flow and redirects to the App with an authorization code.
The PDS Token is encoded in the authorization code.

### 5. App issues a `/token` request

The App now calls the `/token` endpoint to receive a Habitat Token.
Habitat retrieves the PDS Token from decoding the request's authorization code.
Habitat creates a Habitat Token which encodes the PDS Token.
Finally, it responds to the App with the Habitat Token.

### 6. App can now make authenticated resource requests to Habitat

Whenever Habitat receieves a request for some resource, it can validate the attached Habitat Token and decode it to get the PDS Token.
Habitat can then use the PDS Token in its handlers to make authenticated requests to the PDS.
