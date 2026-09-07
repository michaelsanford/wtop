//go:build windows

package websearch

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// httpsUserChoice is where Windows records the user's chosen handler for https.
// The ProgId it holds is the key into HKCR that carries the launch command.
const (
	httpsUserChoice = `Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`
	shellOpenSuffix = `\shell\open\command`
)

// open runs the search on whichever engine the default browser is configured to
// use.  Gecko browsers do this from the command line; Chromium browsers have no
// such flag — an argument is run through URL fixup and a process name becomes
// http://name.exe/ — so their configured engine is read out of the profile and
// the URL is built here.
func open(query string) error {
	exe := defaultBrowser()

	switch browserFamily(exe) {
	case familyGecko:
		if err := launch(exe, geckoArgs(query)); err == nil {
			return nil
		}
	case familyChromium:
		if u := chromiumSearchURL(exe, query); u != "" {
			return openURL(u)
		}
	case familyUnknown:
		// Nothing known about this browser; the fallback engine below still
		// opens in it, because ShellExecute honours the https association.
	}

	return openURL(fallbackSearchURL + url.QueryEscape(query))
}

// launch starts a browser and lets go of it: the browser outlives wtop's
// interest in it, and nothing here needs its exit status.
func launch(exe string, args []string) error {
	// #nosec G204 -- exe comes from the registry, never from the process list,
	// and args are passed as argv with no shell, so the query text cannot escape
	// into a command line.  See CLAUDE.md on the G204 exclusion.
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// chromiumSearchURL resolves the engine exe is configured with and substitutes
// query into it, falling back to the browser's stock engine when the profile
// records none.  Returns "" if exe is not a known Chromium browser or neither
// template yields a usable URL.
func chromiumSearchURL(exe, query string) string {
	prof, ok := chromiumBrowsers[browserBase(exe)]
	if !ok {
		return ""
	}
	if u := searchURLFromTemplate(configuredSearchTemplate(prof), query); u != "" {
		return u
	}
	return searchURLFromTemplate(prof.stockURL, query)
}

// configuredSearchTemplate reads the search-URL template out of the browser's
// active profile, or "" if the profile cannot be read.  Only the search-provider
// URL is taken from the file; nothing else in it is parsed.
func configuredSearchTemplate(prof chromiumProfile) string {
	base := os.Getenv(prof.env)
	if base == "" {
		return ""
	}
	root := filepath.Join(base, prof.root)

	prefsPath := filepath.Join(root, "Preferences")
	if !prof.flat {
		// Both paths are composed from a fixed table entry under a directory named
		// by the environment, never from anything the process list supplies.
		//nolint:errcheck,gosec // G304: an absent Local State just means the Default profile
		state, _ := os.ReadFile(filepath.Join(root, "Local State"))
		prefsPath = filepath.Join(root, parseActiveProfile(state), "Preferences")
	}

	prefs, err := os.ReadFile(prefsPath) //nolint:gosec // G304: see above
	if err != nil {
		return ""
	}
	return parseSearchTemplate(prefs)
}

// defaultBrowser resolves the executable registered to handle https, or "" if
// any step of the lookup fails.
func defaultBrowser() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, httpsUserChoice, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close() //nolint:errcheck
	progID, _, err := k.GetStringValue("ProgId")
	if err != nil || progID == "" {
		return ""
	}

	ck, err := registry.OpenKey(registry.CLASSES_ROOT, progID+shellOpenSuffix, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer ck.Close() //nolint:errcheck
	// The launch command lives in the key's unnamed default value.
	command, _, err := ck.GetStringValue("")
	if err != nil {
		return ""
	}

	exe := parseShellCommand(command)
	if exe == "" {
		return ""
	}
	if _, err := os.Stat(exe); err != nil {
		return ""
	}
	return exe
}

// openURL asks the shell to open a URL with whatever is registered for it.
// ShellExecute is used rather than a subprocess so there is no command line for
// the URL to be parsed out of.
func openURL(u string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(u)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open %s: %w", u, err)
	}
	return nil
}
