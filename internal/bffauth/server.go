package bffauth

type Server interface {
	ValidateToken(token string) (string, error)
}
