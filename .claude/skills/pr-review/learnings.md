## 2026-04-29 | Example Learning
- confidence: high (high | medium | low)
- observation: One sentence observation as concise as possible.
- action: What action should future PR Review skill agents take as a result?

## 2026-09-01 | Trim login handles at the auth server, not the client
- confidence: high
- observation: Handle inputs to Habitat's login flows are trimmed centrally at the server boundaries (oauthserver `resolveLoginHint`, login/password `HandlePasswordLogin`, sap `handleAddSession`) rather than sprinkled across client login forms.
- action: When a login-handle whitespace issue comes up, prefer trimming server-side at the entry points above instead of calling `.trim()` in each UI app.