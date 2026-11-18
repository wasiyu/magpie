package product

import (
	"github.com/yetone/magpie/internal/lazyre"
	"strings"
)

var brandWord = lazyre.New(`(?i)magpie`)
var brandCommand = lazyre.New(`^ (?:app|gui|tray|panel|tui|web|serve|daemon|help|version|licenses|ls|list|provider|providers|presets|model|models|visible|group|groups|account|accounts|quota|quotas|usage|library|lib|plugin|plugins|search|tool|tools|backup|restore|webdav|s3|github|autostart|healthcheck|heap|cpu|goroutines|statusline|claude-code|agy|codex|claude)\b`)
var commandContext = lazyre.New(`(?i)(?:\brun|\btype|\btyping|\blinks) $`)
var upstreamText = lazyre.New(`(?i)what agents call the gateway|model_provider|remote magpie|(?:another|other|that) computer['’]s magpie|magpie community|written for magpie|existing magpie|one server, magpie|server (?:in the library is )?(?:called|named) magpie|has (?:an MCP server called|its own) magpie|folder named magpie`)

// Text brands an application-owned prose template, before formatting user data.
// Package names, paths, protocol identifiers and references to upstream stay as
// written. Never pass a serialized config, response body or user value here.
func Text(s string) string {
	if !Jellytool || upstreamText.MatchString(s) {
		return s
	}
	var out strings.Builder
	last := 0
	for _, at := range brandWord.FindAllStringIndex(s, -1) {
		out.WriteString(s[last:at[0]])
		word := s[at[0]:at[1]]
		before, after := s[:at[0]], s[at[1]:]
		if identifierBefore(before) || identifierAfter(after) {
			out.WriteString(word)
		} else if brandCommand.MatchString(after) || commandContext.MatchString(before) || strings.HasPrefix(after, " %") || strings.HasPrefix(after, " <") || strings.HasSuffix(before, "  ") && strings.HasPrefix(after, " ") {
			out.WriteString(ID)
		} else {
			out.WriteString(Name)
		}
		last = at[1]
	}
	out.WriteString(s[last:])
	return out.String()
}

func identifierBefore(s string) bool {
	return len(s) > 0 && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./\\@-", rune(s[len(s)-1]))
}

func identifierAfter(s string) bool {
	return len(s) > 0 && (strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_/\\-", rune(s[0])) || s[0] == '.' && len(s) > 1 && s[1] != ' ' && s[1] != '\n' || strings.HasPrefix(s, "://"))
}
