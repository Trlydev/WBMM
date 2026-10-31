package sources

import (
	"html"
	"regexp"
	"strings"
)

var (
	reHTMLBlock = regexp.MustCompile(`(?i)<\s*/(p|div|h[1-6]|ul|ol|table)\s*>`)
	reHTMLBreak = regexp.MustCompile(`(?i)<\s*(br|/tr)\s*/?>`)
	reHTMLItem  = regexp.MustCompile(`(?i)<\s*li[^>]*>`)
	reHTMLTag   = regexp.MustCompile(`<[^>]*>`)
	reBBItem    = regexp.MustCompile(`(?i)\[\*\]`)
	reBBTag     = regexp.MustCompile(`(?i)\[/?(b|i|u|s|h[1-6]|url[^\]]*|img|list|olist|quote[^\]]*|code|spoiler|strike|noparse|hr|table|tr|td|th|previewyoutube[^\]]*)\]`)
	reBlank     = regexp.MustCompile(`\n{3,}`)
	reSpaces    = regexp.MustCompile(`[ \t]+`)
)

// HTMLText turns GameBanana's HTML description into plain text.
func HTMLText(s string) string {
	s = reHTMLBlock.ReplaceAllString(s, "\n\n")
	s = reHTMLBreak.ReplaceAllString(s, "\n")
	s = reHTMLItem.ReplaceAllString(s, "\n• ")
	s = reHTMLTag.ReplaceAllString(s, "")
	return tidy(html.UnescapeString(s))
}

// BBText turns a Steam Workshop BBCode description into plain text.
func BBText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reBBItem.ReplaceAllString(s, "• ")
	s = reBBTag.ReplaceAllString(s, "")
	return tidy(s)
}

func tidy(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(reSpaces.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(reBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
