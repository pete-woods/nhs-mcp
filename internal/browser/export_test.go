package browser

import "github.com/go-rod/rod"

// Pages lists the browser's open pages, for tests.
func (a *Authorizer) Pages() (rod.Pages, error) {
	return a.browser.Pages()
}
