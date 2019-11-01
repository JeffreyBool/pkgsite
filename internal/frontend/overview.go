// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package frontend

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"html/template"
	"net/url"
	"path"
	"path/filepath"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
	"golang.org/x/discovery/internal"
)

// OverviewDetails contains all of the data that the readme template
// needs to populate.
type OverviewDetails struct {
	ModulePath    string
	RepositoryURL string
	ReadMe        template.HTML
	ReadMeSource  string
}

// fetchOverviewDetails fetches data for the module version specified by path and version
// from the database and returns a OverviewDetails.
func fetchOverviewDetails(ctx context.Context, ds DataSource, vi *internal.VersionInfo) (_ *OverviewDetails, err error) {
	return &OverviewDetails{
		ModulePath:    vi.ModulePath,
		RepositoryURL: vi.SourceInfo.RepoURL(),
		ReadMeSource:  fileSource(vi.ModulePath, vi.Version, vi.ReadmeFilePath),
		ReadMe:        readmeHTML(vi),
	}, nil
}

// readmeHTML sanitizes readmeContents based on bluemondy.UGCPolicy and returns
// a template.HTML. If readmeFilePath indicates that this is a markdown file,
// it will also render the markdown contents using blackfriday.
func readmeHTML(vi *internal.VersionInfo) template.HTML {
	if len(vi.ReadmeContents) == 0 {
		return ""
	}
	if filepath.Ext(vi.ReadmeFilePath) != ".md" {
		return template.HTML(fmt.Sprintf(`<pre class="readme">%s</pre>`, html.EscapeString(string(vi.ReadmeContents))))
	}

	// bluemonday.UGCPolicy allows a broad selection of HTML elements and
	// attributes that are safe for user generated content. This policy does
	// not whitelist iframes, object, embed, styles, script, etc.
	p := bluemonday.UGCPolicy()

	// Allow width and align attributes on img. This is used to size README
	// images appropriately where used, like the gin-gonic/logo/color.png
	// image in the github.com/gin-gonic/gin README.
	p.AllowAttrs("width", "align").OnElements("img")

	// blackfriday.Run() uses CommonHTMLFlags and CommonExtensions by default.
	renderer := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{Flags: blackfriday.CommonHTMLFlags})
	parser := blackfriday.New(blackfriday.WithExtensions(blackfriday.CommonExtensions))

	// Render HTML similar to blackfriday.Run(), but here we implement a custom
	// Walk function in order to modify image paths in the rendered HTML.
	b := &bytes.Buffer{}
	rootNode := parser.Parse(vi.ReadmeContents)
	rootNode.Walk(func(node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		if node.Type == blackfriday.Image || node.Type == blackfriday.Link {
			translateRelativeLink(node, vi)
		}
		return renderer.RenderNode(b, node, entering)
	})
	return template.HTML(p.SanitizeReader(b).String())
}

// translateRelativeLink modifies a blackfriday.Node to convert relative image
// paths to absolute paths.
//
// Markdown files, such as the Go README, sometimes use relative image paths to
// image files inside the repository. As the discovery site doesn't host the
// full repository content, in order for the image to render, we need to
// convert the relative path to an absolute URL to a hosted image.
func translateRelativeLink(node *blackfriday.Node, vi *internal.VersionInfo) {
	destURL, err := url.Parse(string(node.LinkData.Destination))
	if err != nil || destURL.IsAbs() {
		return
	}
	// Paths are relative to the README location.
	destPath := path.Join(path.Dir(vi.ReadmeFilePath), path.Clean(destURL.Path))
	var newURL string
	if node.Type == blackfriday.Image {
		newURL = vi.SourceInfo.RawURL(destPath)
	} else {
		newURL = vi.SourceInfo.FileURL(destPath)
	}
	if newURL != "" {
		node.LinkData.Destination = []byte(newURL)
	}
}