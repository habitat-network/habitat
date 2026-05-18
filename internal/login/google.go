package login

// type googleProvider struct {
// 	loginMethod string
// }
//
// func NewGoogleProvider() Provider {
// 	return &googleProvider{
// 		loginMethod: "google",
// 	}
// }
//
// func (p *googleProvider) LoginMethod() string { return p.loginMethod }
//
// func (p *googleProvider) Authorize(ctx context.Context, id *identity.Identity, loginID string) (string, []byte, error) {
// 	if loginID == "" {
// 		return "", nil, fmt.Errorf("no google email configured for org %s", id.DID)
// 	}
// 	// TODO: implement Google OAuth initiation with login_hint=loginID
// 	_ = loginID
// 	return "", nil, fmt.Errorf("google provider not yet implemented")
// }
//
// func (p *googleProvider) Exchange(_ context.Context, _ syntax.DID, _, _ string, _ []byte) error {
// 	// TODO: implement Google OAuth callback handling
// 	return fmt.Errorf("google provider not yet implemented")
// }
