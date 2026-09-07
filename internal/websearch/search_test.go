package websearch

import (
	"errors"
	"slices"
	"testing"
)

func TestParseShellCommand(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"quoted path with spaces",
			`"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe" --single-argument %1`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		},
		{
			"quoted path with no arguments",
			`"C:\Program Files\Mozilla Firefox\firefox.exe"`,
			`C:\Program Files\Mozilla Firefox\firefox.exe`,
		},
		{
			"unquoted path with an argument",
			`C:\Windows\System32\iexplore.exe %1`,
			`C:\Windows\System32\iexplore.exe`,
		},
		{"unquoted bare path", `C:\bin\browser.exe`, `C:\bin\browser.exe`},
		{"leading whitespace is trimmed", `   "C:\a\b.exe" "%1"`, `C:\a\b.exe`},
		{"unterminated quote", `"C:\a\b.exe`, ""},
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseShellCommand(tc.in); got != tc.want {
				t.Errorf("parseShellCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeQuery(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain name", "chrome.exe", "chrome.exe"},
		{"parent and child", "GoogleDriveFS.exe msedgewebview2.exe", "GoogleDriveFS.exe msedgewebview2.exe"},
		{"surrounding whitespace", "  chrome.exe \n", "chrome.exe"},
		{"internal whitespace collapses", "a.exe \t  b.exe", "a.exe b.exe"},
		{"leading dash is stripped", "--headless.exe", "headless.exe"},
		{"an inner dash survives", "well-known.exe", "well-known.exe"},
		{"empty", "", ""},
		{"dashes only", "---", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeQuery(tc.in); got != tc.want {
				t.Errorf("sanitizeQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBrowserFamily(t *testing.T) {
	tests := []struct {
		name string
		exe  string
		want family
	}{
		{"edge", `C:\p\msedge.exe`, familyChromium},
		{"chrome", `C:\p\chrome.exe`, familyChromium},
		{"brave", `C:\p\brave.exe`, familyChromium},
		{"opera", `C:\p\opera.exe`, familyChromium},
		{"firefox", `C:\p\firefox.exe`, familyGecko},
		{"librewolf", `C:\p\librewolf.exe`, familyGecko},
		{"case-insensitive", `C:\p\MsEdge.EXE`, familyChromium},
		{"unknown browser", `C:\p\safari.exe`, familyUnknown},
		{"empty path", "", familyUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := browserFamily(tc.exe); got != tc.want {
				t.Errorf("browserFamily(%q) = %v, want %v", tc.exe, got, tc.want)
			}
		})
	}
}

func TestGeckoArgs(t *testing.T) {
	want := []string{"--search", "q.exe"}
	if got := geckoArgs("q.exe"); !slices.Equal(got, want) {
		t.Errorf("geckoArgs = %v, want %v", got, want)
	}
}

// Every stock engine must itself survive template substitution, or a profile
// that records no engine would fall all the way through to fallbackSearchURL.
func TestStockURLsAreUsableTemplates(t *testing.T) {
	for name, prof := range chromiumBrowsers {
		if got := searchURLFromTemplate(prof.stockURL, "a.exe"); got == "" {
			t.Errorf("%s stock template %q yielded no URL", name, prof.stockURL)
		}
	}
}

func TestParseSearchTemplate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"chrome-style template_url_data",
			`{"default_search_provider_data":{"template_url_data":{"url":"https://www.google.com/search?q={searchTerms}"}}}`,
			"https://www.google.com/search?q={searchTerms}",
		},
		{
			// Edge keeps the authoritative copy in an encrypted enclave blob and
			// mirrors a readable one, so template_url_data reads back as null.
			"edge-style mirrored data",
			`{"default_search_provider_data":{"template_url_data":null,"mirrored_template_url_data":{"url":"https://duckduckgo.com/?q={searchTerms}"}}}`,
			"https://duckduckgo.com/?q={searchTerms}",
		},
		{
			"template_url_data wins over the mirror",
			`{"default_search_provider_data":{"template_url_data":{"url":"https://a/?q={searchTerms}"},"mirrored_template_url_data":{"url":"https://b/?q={searchTerms}"}}}`,
			"https://a/?q={searchTerms}",
		},
		{
			"no engine recorded",
			`{"default_search_provider_data":{"default_search_provider":{"guid":"x"}}}`,
			"",
		},
		{"unrelated preferences", `{"profile":{"name":"Default"}}`, ""},
		{"malformed json", `{"default_search_provider_data":`, ""},
		{"empty file", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseSearchTemplate([]byte(tc.in)); got != tc.want {
				t.Errorf("parseSearchTemplate = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseActiveProfile(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"named profile", `{"profile":{"last_used":"Profile 3"}}`, "Profile 3"},
		{"explicit default", `{"profile":{"last_used":"Default"}}`, "Default"},
		{"key absent", `{"profile":{}}`, "Default"},
		{"file absent", "", "Default"},
		{"malformed json", `{"profile":`, "Default"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseActiveProfile([]byte(tc.in)); got != tc.want {
				t.Errorf("parseActiveProfile = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSearchURLFromTemplate(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want string
	}{
		{
			"plain template",
			"https://duckduckgo.com/?q={searchTerms}",
			"https://duckduckgo.com/?q=a.exe+b.exe",
		},
		{
			"query is escaped",
			"https://www.bing.com/search?q={searchTerms}",
			"https://www.bing.com/search?q=a.exe+b.exe",
		},
		{
			"internal placeholders are dropped",
			"https://www.google.com/search?q={searchTerms}&{google:RLZ}{google:originalQueryForSuggestion}ie=UTF-8",
			"https://www.google.com/search?q=a.exe+b.exe&ie=UTF-8",
		},
		{
			"trailing placeholder leaves no dangling separator",
			"https://e.com/?q={searchTerms}&{google:RLZ}",
			"https://e.com/?q=a.exe+b.exe",
		},
		{
			// A template rooted in a placeholder cannot be reassembled outside
			// the browser; the caller falls back to the stock engine.
			"placeholder prefix is rejected",
			"{google:baseURL}search?q={searchTerms}",
			"",
		},
		{"no searchTerms token", "https://e.com/?q=fixed", ""},
		{"not a URL", "notaurl{searchTerms}", ""},
		{"unsupported scheme", "ftp://e.com/?q={searchTerms}", ""},
		{"empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := searchURLFromTemplate(tc.tmpl, "a.exe b.exe"); got != tc.want {
				t.Errorf("searchURLFromTemplate(%q) = %q, want %q", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestSearch_RejectsAnEmptyQuery(t *testing.T) {
	for _, q := range []string{"", "   ", "--"} {
		if err := Search(q); !errors.Is(err, ErrEmptyQuery) {
			t.Errorf("Search(%q) = %v, want ErrEmptyQuery", q, err)
		}
	}
}
