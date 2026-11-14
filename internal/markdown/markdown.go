package markdown

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

var (
	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Linkify),
		goldmark.WithRendererOptions(gmhtml.WithHardWraps()),
	)
	policy = bluemonday.UGCPolicy()
)

func init() {
	policy.AllowRelativeURLs(true)
	policy.RequireNoFollowOnLinks(true)
}

func Render(src string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return policy.Sanitize(buf.String())
}
