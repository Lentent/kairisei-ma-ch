package httpapi

// bannerURL uses the immutable startup inventory shared by all accounts.
// The fallback preserves isolated handlers that have no resource inventory.
func (a *API) bannerURL(path string) string {
	if versioned := a.bannerPaths[path]; versioned != "" {
		path = versioned
	}
	return a.baseURL + path
}
