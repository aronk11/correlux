package app

import (
	"context"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/aronk11/correlux/internal/buildinfo"
	"github.com/aronk11/correlux/internal/ui/async"
	"github.com/aronk11/correlux/internal/ui/theme"
	"github.com/aronk11/correlux/internal/update"
)

// Correlux says when it is out of date, and it is the only thing it ever asks
// anybody but a Kubernetes API server.
//
// The check runs once a day, in the background, off the answer cached beside
// the configuration file, and it never delays anything: a start that cannot
// reach the feed looks exactly like one that did not need to. Where it went is
// stated on the session screen, next to the switch that turns it off, because
// an outbound request nobody can find is an outbound request nobody agreed to.

// updateCheckedMsg carries the answer, or the reason there is none. announce
// separates the two kinds of check: one the user asked for, which has to say
// something, and the one that runs on its own, which must not.
type updateCheckedMsg struct {
	gen      uint64
	release  update.Release
	announce bool
}

// updateCachePath is where the answer is remembered: beside the configuration
// file, whether or not that file exists yet.
func (m *Model) updateCachePath() string {
	if m.configPath == "" {
		return ""
	}
	return update.CachePath(filepath.Dir(m.configPath))
}

// checkForUpdate asks whether there is a newer Correlux.
//
// force is an explicit request from the palette, which ignores both the cache
// and update.check. Air-gapped mode always takes precedence.
func (m *Model) checkForUpdate(force bool) tea.Cmd {
	if m.cfg.AirGapped {
		return nil
	}
	current := buildinfo.Get().Version
	if !force {
		if !m.cfg.Update.Check {
			return nil
		}
		// A build with no version cannot be ranked against a release, so the
		// request would tell the user nothing and the feed something.
		if !update.Rankable(current) {
			return nil
		}
	}

	gen := m.updateCheck.Start()
	path := m.updateCachePath()
	return func() tea.Msg {
		return updateCheckedMsg{
			gen:      gen,
			release:  update.Check(context.Background(), path, current, force),
			announce: force,
		}
	}
}

// applyUpdateCheck stores what the check learned. A failure is remembered as a
// failure rather than shown: nothing about a release feed is worth interrupting
// somebody looking at a broken cluster.
func (m *Model) applyUpdateCheck(msg updateCheckedMsg) tea.Cmd {
	if msg.release.Err != nil {
		m.updateCheck.Fail(msg.gen, msg.release.Err)
	} else {
		m.updateCheck.Succeed(msg.gen, msg.release)
	}
	m.rebuildCommands()
	if !msg.announce {
		return nil
	}
	text, status := m.noticeForUpdate(msg.release)
	m.notice(text, status)
	return m.expireNotice()
}

// newerVersion is the release worth telling the user about, empty when this
// build is current, unrankable, or nothing was learned.
func (m *Model) newerVersion() string {
	if m.cfg.AirGapped {
		return ""
	}
	release := m.updateCheck.Get()
	if release.Version == "" {
		return ""
	}
	if !update.Newer(buildinfo.Get().Version, release.Version) {
		return ""
	}
	return release.Version
}

// updateHeaderLabel is what the header shows beside the version: the newer one,
// and nothing at all the rest of the time. A header that reports "up to date"
// spends a permanent line on the answer nobody needed.
func (m *Model) updateHeaderLabel() string {
	if newer := m.newerVersion(); newer != "" {
		return m.theme.Glyphs.Arrow + " " + newer
	}
	return ""
}

// updateSummary is the session screen's account of the check: what it found,
// or why there is nothing to report. This is the one place all four states are
// distinguishable, which is the whole rule (ADR 5).
func (m *Model) updateSummary() (value string, status theme.Status) {
	current := buildinfo.Get().Version
	switch {
	case m.cfg.AirGapped:
		return current + " — air-gapped; update checks disabled", theme.StatusUnknown
	case !m.cfg.Update.Check && !m.updateCheck.HasValue():
		return current + " — update check off", theme.StatusUnknown
	case !update.Rankable(current):
		return current + " — a build with no version is not compared", theme.StatusUnknown
	}

	switch m.updateCheck.State() {
	case async.Idle:
		return current + " — not checked yet", theme.StatusUnknown
	case async.Loading:
		return current + " — checking…", theme.StatusUnknown
	case async.Failed:
		// Not a problem with Correlux and not the user's to fix: said once,
		// where somebody who wondered can find it.
		return current + " — could not check: " + shortError(m.updateCheck.Err()), theme.StatusUnknown
	}

	if newer := m.newerVersion(); newer != "" {
		return current + " — " + newer + " is available", theme.StatusWarning
	}
	return current + " — up to date", theme.StatusHealthy
}

// updateNote points at the release page, or at the setting, depending on which
// one the reader needs next.
//
// The page is given without its scheme and on a line of its own: a panel is
// narrow, a URL is one unbreakable word, and one wrapped in the middle is a
// URL nobody can copy.
func (m *Model) updateNote() string {
	if m.cfg.AirGapped {
		return "Air-gapped mode blocks automatic and manual release checks. Transfer updates offline."
	}
	if m.newerVersion() != "" {
		return shortURL(m.updateCheck.Get().URL)
	}
	if !m.cfg.Update.Check {
		return "Update check off. Turn it on with update.check: true."
	}
	return "Once a day, against the public release feed, and nothing else is sent. " +
		"Off with update.check: false."
}

// shortURL drops the scheme, which every terminal and every human puts back.
func shortURL(url string) string {
	return strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
}

// updateSubtitleForPalette says what the command would do, in the state it is
// actually in.
func (m *Model) updateSubtitleForPalette() string {
	if newer := m.newerVersion(); newer != "" {
		return newer + " is available — ask again"
	}
	value, _ := m.updateSummary()
	return value
}

// checkUpdateNow asks the feed on the spot and says what came back, because a
// keystroke that answers silently reads as a keystroke that did nothing.
func (m *Model) checkUpdateNow() tea.Cmd {
	if m.cfg.AirGapped {
		m.notice("Update checks are disabled in air-gapped mode", theme.StatusWarning)
		return m.expireNotice()
	}
	m.notice("Asking the release feed…", theme.StatusUnknown)
	return tea.Batch(m.checkForUpdate(true), m.expireNotice())
}

// noticeForUpdate is what an explicitly requested check says when it lands.
func (m *Model) noticeForUpdate(release update.Release) (string, theme.Status) {
	switch {
	case release.Err != nil:
		return "Could not reach the release feed: " + shortError(release.Err), theme.StatusWarning
	case update.Newer(buildinfo.Get().Version, release.Version):
		return release.Version + " is available — " + release.URL, theme.StatusWarning
	default:
		return "Correlux is up to date", theme.StatusHealthy
	}
}
