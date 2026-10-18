package remoteitem

import (
	"net/url"
	"strings"
)

// Hoster is one entry of a provider's hoster catalogue.
type Hoster struct {
	Domains []string
	Up      bool
}

// MatchesHoster reports whether the URL's host is a catalogued domain, or a
// subdomain of one, of a hoster that is up (spec §5.5 step 2).
func MatchesHoster(rawURL string, hosters []Hoster) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	for _, hoster := range hosters {
		if !hoster.Up {
			continue
		}
		for _, domain := range hoster.Domains {
			domain = strings.ToLower(strings.TrimSpace(domain))
			if domain != "" && (host == domain || strings.HasSuffix(host, "."+domain)) {
				return true
			}
		}
	}
	return false
}
