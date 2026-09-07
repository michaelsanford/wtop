// Package websearch opens a web search for a free-standing query in the user's
// default browser.  The query is composed by the caller from process names read
// out of the system process table; nothing here ever sees text the user typed.
package websearch

import (
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

// fallbackSearchURL is the last resort: used when the default browser cannot be
// resolved, or when its configured engine cannot be read.  Handing the browser
// the query itself would be preferable, but no Chromium-family browser has a
// command line that means "search" — arguments are run through URL fixup, which
// turns a process name into http://name.exe/ — so the engine has to be resolved
// and the URL built here.
const fallbackSearchURL = "https://duckduckgo.com/?q="

// searchTermsToken is the placeholder Chromium substitutes the query into.
const searchTermsToken = "{searchTerms}"

// ErrEmptyQuery is returned when there is nothing to search for.
var ErrEmptyQuery = errors.New("empty search query")

// Search opens a web search for query in the default browser.
func Search(query string) error {
	q := sanitizeQuery(query)
	if q == "" {
		return ErrEmptyQuery
	}
	return open(q)
}

// sanitizeQuery normalises whitespace and strips leading dashes.  Firefox parses
// a leading '-' as a command-line flag, and an image name is free to start with
// one.
func sanitizeQuery(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimLeft(s, "-")
}

// parseShellCommand extracts the executable from a registry shell\open\command
// value.  The quoted form ("C:\...\msedge.exe" -- "%1") is by far the common one,
// but the value is not required to be quoted, so fall back to the first
// whitespace-delimited token.
func parseShellCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if cmd[0] == '"' {
		if end := strings.IndexByte(cmd[1:], '"'); end >= 0 {
			return cmd[1 : 1+end]
		}
		return ""
	}
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

// family identifies how a browser has to be driven.  The two families differ
// fundamentally: Gecko will run a search from the command line, Chromium will
// not and has to have its configured engine read out of its profile instead.
type family int

const (
	familyUnknown family = iota
	familyChromium
	familyGecko
)

// chromiumProfile says where a browser keeps its profile and what engine it
// ships with.  root is relative to the directory named by env; flat marks Opera,
// which keeps Preferences in the profile root rather than in a per-profile
// subdirectory.
type chromiumProfile struct {
	env      string
	root     string
	flat     bool
	stockURL string
}

var chromiumBrowsers = map[string]chromiumProfile{
	"msedge":   {"LOCALAPPDATA", `Microsoft\Edge\User Data`, false, "https://www.bing.com/search?q=" + searchTermsToken},
	"chrome":   {"LOCALAPPDATA", `Google\Chrome\User Data`, false, "https://www.google.com/search?q=" + searchTermsToken},
	"brave":    {"LOCALAPPDATA", `BraveSoftware\Brave-Browser\User Data`, false, "https://search.brave.com/search?q=" + searchTermsToken},
	"vivaldi":  {"LOCALAPPDATA", `Vivaldi\User Data`, false, "https://www.bing.com/search?q=" + searchTermsToken},
	"chromium": {"LOCALAPPDATA", `Chromium\User Data`, false, "https://www.google.com/search?q=" + searchTermsToken},
	"opera":    {"APPDATA", `Opera Software\Opera Stable`, true, "https://www.google.com/search?q=" + searchTermsToken},
}

var geckoBrowsers = map[string]bool{"firefox": true, "waterfox": true, "librewolf": true}

// browserBase reduces an executable path to the lower-case name the family
// tables are keyed by.
func browserBase(exe string) string {
	return strings.TrimSuffix(strings.ToLower(filepath.Base(exe)), ".exe")
}

// browserFamily classifies exe.
func browserFamily(exe string) family {
	base := browserBase(exe)
	if _, ok := chromiumBrowsers[base]; ok {
		return familyChromium
	}
	if geckoBrowsers[base] {
		return familyGecko
	}
	return familyUnknown
}

// geckoArgs returns the arguments that make a Gecko browser search for query
// with its own configured engine.  The --search separator also keeps a query
// from being read as a flag.
func geckoArgs(query string) []string {
	return []string{"--search", query}
}

// chromiumPrefs is the fragment of a Chromium Preferences file that carries the
// search engine.  Chrome and Brave populate template_url_data; Edge keeps the
// authoritative copy in an encrypted enclave blob and mirrors a readable one
// into mirrored_template_url_data, so both have to be tried.
type chromiumPrefs struct {
	DefaultSearchProviderData struct {
		TemplateURLData struct {
			URL string `json:"url"`
		} `json:"template_url_data"`
		MirroredTemplateURLData struct {
			URL string `json:"url"`
		} `json:"mirrored_template_url_data"`
	} `json:"default_search_provider_data"`
}

// parseSearchTemplate pulls the search-URL template out of a Chromium
// Preferences file.  An absent key is normal — a profile that has never changed
// engine does not record one — and yields "", which the caller answers with the
// browser's stock engine.
func parseSearchTemplate(prefs []byte) string {
	var p chromiumPrefs
	if err := json.Unmarshal(prefs, &p); err != nil {
		return ""
	}
	if u := p.DefaultSearchProviderData.TemplateURLData.URL; u != "" {
		return u
	}
	return p.DefaultSearchProviderData.MirroredTemplateURLData.URL
}

// localState is the fragment of Chromium's Local State that names the profile
// directory last in use.
type localState struct {
	Profile struct {
		LastUsed string `json:"last_used"`
	} `json:"profile"`
}

// parseActiveProfile returns the profile directory to read Preferences from.
// "Default" is Chromium's own name for the first profile and the right answer
// whenever Local State is missing or says nothing.
func parseActiveProfile(state []byte) string {
	var s localState
	if err := json.Unmarshal(state, &s); err != nil {
		return "Default"
	}
	if s.Profile.LastUsed == "" {
		return "Default"
	}
	return s.Profile.LastUsed
}

// searchURLFromTemplate substitutes query into a Chromium search template.
// Templates carry Chromium-internal placeholders besides {searchTerms} —
// {google:RLZ}, {inputEncoding} and friends — which expand to nothing useful
// outside the browser and are simply dropped.  A template whose *prefix* is a
// placeholder ({google:baseURL}search?q=...) cannot be salvaged that way, so the
// result is validated as an absolute http(s) URL and "" returned if it is not.
func searchURLFromTemplate(tmpl, query string) string {
	if !strings.Contains(tmpl, searchTermsToken) {
		return ""
	}
	u := strings.ReplaceAll(tmpl, searchTermsToken, url.QueryEscape(query))
	u = tidyQuery(stripPlaceholders(u))

	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}
	return u
}

// stripPlaceholders removes every remaining {...} token.
func stripPlaceholders(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for {
		i := strings.IndexByte(s, '{')
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		s = s[i+j+1:]
	}
}

// tidyQuery cleans up the separators left behind by dropped placeholders, so
// "?q=x&{a}&{b}c=1" does not become "?q=x&&c=1".
func tidyQuery(u string) string {
	for strings.Contains(u, "&&") {
		u = strings.ReplaceAll(u, "&&", "&")
	}
	u = strings.ReplaceAll(u, "?&", "?")
	return strings.TrimRight(u, "&?")
}
