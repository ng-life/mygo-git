package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	html "golang.org/x/net/html"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func isPreviewDocument(filename string) bool {
	kind := fileType(filename).Kind
	return kind == "Markdown" || kind == "HTML"
}

var markdownPreview = goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID()), goldmark.WithRendererOptions(goldhtml.WithUnsafe()))

const previewPolicy = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline' data:; img-src data: https:; font-src data:; connect-src 'none'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'"
const previewStyle = `:root{color-scheme:dark}html,body{background:#2b2b2b;color:#bbb}body{font:14px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:0;padding:24px;overflow-wrap:anywhere}body.markdown{max-width:960px;margin:auto}h1,h2,h3{line-height:1.3;color:#ddd}h1,h2{padding-bottom:.3em;border-bottom:1px solid #45494a}a{color:#75a9df}img{max-width:100%;height:auto}pre,code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;font-size:12px}pre{background:#242628;padding:14px;overflow:auto;border:1px solid #45494a;border-radius:3px}code{background:#343638;padding:2px 4px}pre code{background:none;padding:0}blockquote{border-left:3px solid #626668;padding-left:16px;color:#999;margin-left:0}table{border-collapse:collapse;display:block;overflow:auto}th,td{padding:6px 12px;border:1px solid #515658}th{background:#343638}hr{border:0;border-top:1px solid #515658}input[type=checkbox]{accent-color:#589df6}svg{max-width:100%}`

type previewResources struct {
	count, bytes int
	cache        map[string]string
}

var cssURLPattern = regexp.MustCompile(`(?i)url\(\s*([^)]*)\)`)

func previewAsset(ctx context.Context, source CodeSource, relative string) ([]byte, string, error) {
	u, err := url.Parse(relative)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "/") {
		return nil, "", fmt.Errorf("not a relative asset")
	}
	asset := path.Clean(path.Join(path.Dir(source.Path), u.Path))
	if !validWorkingPath(asset) || strings.Contains("/"+asset+"/", "/.git/") {
		return nil, "", fmt.Errorf("asset is outside the document repository")
	}
	const limit = 8 * 1024 * 1024
	var data []byte
	if source.Revision == "" {
		full, e := safeCodePath(source.Repository, asset)
		if e != nil {
			return nil, "", e
		}
		info, e := os.Stat(full)
		if e != nil {
			return nil, "", e
		}
		if !info.Mode().IsRegular() || info.Size() > limit {
			return nil, "", fmt.Errorf("asset exceeds preview limits")
		}
		data, err = os.ReadFile(full)
	} else {
		ref := source.Revision + ":" + asset
		if source.Revision == ":" {
			ref = ":" + asset
		}
		object, e := gitContext(ctx, source.Repository, "rev-parse", "--verify", "--end-of-options", ref)
		if e != nil {
			return nil, "", e
		}
		sizeText, e := gitContext(ctx, source.Repository, "cat-file", "-s", object)
		if e != nil {
			return nil, "", e
		}
		size, e := strconv.Atoi(sizeText)
		if e != nil || size > limit {
			return nil, "", fmt.Errorf("asset exceeds preview limits")
		}
		data, err = rawGitContext(ctx, source.Repository, "cat-file", "blob", object)
	}
	return data, asset, err
}
func (r *previewResources) embed(ctx context.Context, source CodeSource, value string) string {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if u.Scheme == "data" || u.Scheme == "https" || u.Scheme == "http" || u.Host != "" {
		return value
	}
	if u.IsAbs() || strings.HasPrefix(value, "#") || value == "" {
		if strings.HasPrefix(value, "#") {
			return value
		}
		return ""
	}
	key := source.Repository + "\x00" + source.Revision + "\x00" + path.Dir(source.Path) + "\x00" + value
	if cached, ok := r.cache[key]; ok {
		return cached
	}
	if ctx.Err() != nil || r.count >= 64 || r.bytes >= 32*1024*1024 {
		return ""
	}
	data, asset, err := previewAsset(ctx, source, value)
	if err != nil {
		return ""
	}
	if r.bytes+len(data) > 32*1024*1024 {
		return ""
	}
	r.count++
	r.bytes += len(data)
	contentType := mime.TypeByExtension(filepath.Ext(asset))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	encoded := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	r.cache[key] = encoded
	return encoded
}
func (r *previewResources) css(ctx context.Context, source CodeSource, css string) string {
	result := cssURLPattern.ReplaceAllStringFunc(css, func(raw string) string {
		match := cssURLPattern.FindStringSubmatch(raw)
		value := strings.Trim(strings.TrimSpace(match[1]), "\"'")
		return "url(\"" + r.embed(ctx, source, value) + "\")"
	})
	return strings.ReplaceAll(result, "<", `\3c `)
}
func renderDocumentPreview(ctx context.Context, source CodeSource, content string) (string, error) {
	isMarkdown := fileType(source.Path).Kind == "Markdown"
	if isMarkdown {
		var out bytes.Buffer
		if err := markdownPreview.Convert([]byte(content), &out); err != nil {
			return "", err
		}
		content = out.String()
	}
	document, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return "", err
	}
	resources := previewResources{cache: map[string]string{}}
	blocked := map[string]bool{"script": true, "iframe": true, "frame": true, "frameset": true, "object": true, "embed": true, "applet": true, "base": true, "meta": true}
	var clean func(*html.Node)
	clean = func(parent *html.Node) {
		for node := parent.FirstChild; node != nil; {
			next := node.NextSibling
			if node.Type == html.ElementNode && blocked[node.Data] {
				parent.RemoveChild(node)
				node = next
				continue
			}
			if node.Type == html.ElementNode {
				attrs := node.Attr[:0]
				for _, attr := range node.Attr {
					key := strings.ToLower(attr.Key)
					if strings.HasPrefix(key, "on") || key == "srcdoc" || key == "srcset" || key == "action" || key == "formaction" {
						continue
					}
					if key == "href" || key == "src" || key == "poster" || key == "xlink:href" {
						u, e := url.Parse(strings.TrimSpace(attr.Val))
						if e != nil || u.Scheme == "javascript" || u.Scheme == "vbscript" || u.Scheme == "file" {
							continue
						}
					}
					if key == "src" || key == "poster" {
						attr.Val = resources.embed(ctx, source, attr.Val)
					}
					if key == "style" {
						attr.Val = resources.css(ctx, source, attr.Val)
					}
					attrs = append(attrs, attr)
				}
				node.Attr = attrs
				if node.Data == "input" {
					node.Attr = append(node.Attr, html.Attribute{Key: "disabled", Val: ""})
				}
				if node.Data == "style" {
					for text := node.FirstChild; text != nil; text = text.NextSibling {
						if text.Type == html.TextNode {
							text.Data = resources.css(ctx, source, text.Data)
						}
					}
				}
				if node.Data == "link" {
					href, rel := "", ""
					for _, attr := range node.Attr {
						if attr.Key == "href" {
							href = attr.Val
						}
						if attr.Key == "rel" {
							rel = attr.Val
						}
					}
					if strings.EqualFold(rel, "stylesheet") {
						data, asset, e := previewAsset(ctx, source, href)
						if e == nil && resources.count < 64 && resources.bytes+len(data) <= 32*1024*1024 {
							resources.count++
							resources.bytes += len(data)
							style := &html.Node{Type: html.ElementNode, Data: "style"}
							cssSource := source
							cssSource.Path = asset
							style.AppendChild(&html.Node{Type: html.TextNode, Data: resources.css(ctx, cssSource, string(data))})
							parent.InsertBefore(style, node)
						}
						parent.RemoveChild(node)
						node = next
						continue
					}
				}
			}
			clean(node)
			node = next
		}
	}
	clean(document)
	var head, body *html.Node
	var find func(*html.Node)
	find = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "head" {
			head = node
		}
		if node.Type == html.ElementNode && node.Data == "body" {
			body = node
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(document)
	if head == nil {
		return "", fmt.Errorf("preview document has no head")
	}
	style := &html.Node{Type: html.ElementNode, Data: "style"}
	style.AppendChild(&html.Node{Type: html.TextNode, Data: previewStyle})
	head.InsertBefore(style, head.FirstChild)
	for _, attrs := range [][]html.Attribute{{{Key: "name", Val: "viewport"}, {Key: "content", Val: "width=device-width,initial-scale=1"}}, {{Key: "charset", Val: "utf-8"}}, {{Key: "http-equiv", Val: "Content-Security-Policy"}, {Key: "content", Val: previewPolicy}}} {
		head.InsertBefore(&html.Node{Type: html.ElementNode, Data: "meta", Attr: attrs}, head.FirstChild)
	}
	if isMarkdown && body != nil {
		body.Attr = append(body.Attr, html.Attribute{Key: "class", Val: "markdown"})
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = html.Render(&out, document)
	return out.String(), err
}
