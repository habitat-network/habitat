import { Schema } from "effect";

// The app's expected failures. Every server function runs as an Effect (see
// runEffect in ./run.ts), and these are the errors it can fail with: the
// runner turns each one into the right response (a redirect to /login, or an
// Error whose message the page shows) and logs it at the right level.
// Anything else that goes wrong is a defect: logged in full, and shown to the
// browser only as a generic message.

// NotSignedIn: the request has no session cookie naming a member.
export class NotSignedIn extends Schema.TaggedError<NotSignedIn>()(
  "NotSignedIn",
  {},
) {}

// SessionExpired: pear no longer accepts the OAuth session the app holds for
// `did` (a call came back 401 after the OAuth client's own refresh-and-retry,
// or the session couldn't be restored at all). When `did` is the signed-in
// member, the runner signs them out; when it's an org, the org needs
// reconnecting, which the member's own login has nothing to do with.
export class SessionExpired extends Schema.TaggedError<SessionExpired>()(
  "SessionExpired",
  { did: Schema.String, message: Schema.String },
) {}

// HabitatError: a call to the Habitat instance failed for any other reason.
// `status` is the HTTP status (0 when the request never completed) and
// `error` the XRPC error name, when pear sent one.
export class HabitatError extends Schema.TaggedError<HabitatError>()(
  "HabitatError",
  {
    nsid: Schema.String,
    status: Schema.Number,
    error: Schema.optional(Schema.String),
    message: Schema.String,
    // pear's own explanation for a rejected request (the `message` of its
    // error body), which is written for the person who made the request.
    detail: Schema.optional(Schema.String),
  },
) {}

// NotAMember: the member asked for a group they don't belong to. A group
// they can't see looks the same as one that doesn't exist.
export class NotAMember extends Schema.TaggedError<NotAMember>()("NotAMember", {
  org: Schema.String,
}) {}

// OrgNotConnected: the work needs the group's own OAuth session (creating
// its doc and file spaces, minting delegation tokens), and no admin has
// connected the group to this app yet.
export class OrgNotConnected extends Schema.TaggedError<OrgNotConnected>()(
  "OrgNotConnected",
  { org: Schema.String },
) {}

export class Forbidden extends Schema.TaggedError<Forbidden>()("Forbidden", {
  message: Schema.String,
}) {}

export class NotFound extends Schema.TaggedError<NotFound>()("NotFound", {
  message: Schema.String,
}) {}

export class BadRequest extends Schema.TaggedError<BadRequest>()("BadRequest", {
  message: Schema.String,
}) {}

// Redirect sends the browser somewhere else instead of answering, like
// TanStack Router's redirect() but as a typed failure an Effect can return.
export class Redirect extends Schema.TaggedError<Redirect>()("Redirect", {
  to: Schema.String,
}) {}

export type AppError =
  | NotSignedIn
  | SessionExpired
  | HabitatError
  | NotAMember
  | OrgNotConnected
  | Forbidden
  | NotFound
  | BadRequest
  | Redirect;

// userMessage is what the browser is told about an expected failure.
export function userMessage(error: AppError): string {
  switch (error._tag) {
    case "NotSignedIn":
      return "Sign in to continue";
    case "SessionExpired":
      return "Your session with this group expired. An admin needs to reconnect it.";
    case "HabitatError":
      // A 4xx is pear saying what was wrong with the request; the raw
      // message is for the logs.
      return error.status >= 400 && error.status < 500 && error.detail
        ? error.detail
        : error.message;
    case "NotAMember":
      return "You aren't a member of that group";
    case "OrgNotConnected":
      return "An admin needs to connect this group to the app first";
    case "Forbidden":
    case "NotFound":
    case "BadRequest":
      return error.message;
    case "Redirect":
      return `Redirecting to ${error.to}`;
  }
}
