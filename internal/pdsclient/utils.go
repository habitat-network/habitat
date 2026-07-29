package pdsclient

import "strings"

func doesHandleBelongToDomain(handle string, domain string) bool {
	return strings.HasSuffix(handle, "."+domain) || handle == domain
}
