// Package recon scrapes URLs for OpenGraph information.
package recon

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/html"
)

// Parser is the client object and holds the relevant information needed when parsing a URL
type Parser struct {
	customClient       func() *http.Client
	imageLookupTimeout time.Duration
	tokenMaxBuffer     int
	client             *http.Client
	headers            http.Header
}

type parseJob struct {
	request        *http.Request
	requestURL     *url.URL
	response       *http.Response
	metaTags       []metaTag
	imgTags        []imgTag
	tokenMaxBuffer int
}

// Result is what comes back from a Parse
type Result struct {
	// URL is either the URL as-passed or the defined URL (via og:url) if present
	URL string `json:"url"`

	// Host is the domain of the URL as-passed or the defined URL if present
	Host string `json:"host"`

	// Site is the name of the site as defined via og:site_name or site_name
	Site string `json:"site_name"`

	// Title is the title of the page as defined via og:title or title
	Title string `json:"title"`

	// Type is the type of the page (article, video, etc.) as defined via og:type or type.
	Type string `json:"type"`

	// Description is the description of the page as defined via og:description or description.
	Description string `json:"description"`

	// Author is the author of the page as defined via og:author or author.
	Author string `json:"author"`

	// Publisher is the publisher of the page as defined via og:publisher or publisher.
	Publisher string `json:"publisher"`

	// Images is the collection of images parsed from the page using either og:image meta tags or <img> tags.
	Images []Image `json:"images"`

	// Scraped is the time when the page was scraped (or the time Parse was run).
	Scraped time.Time `json:"scraped"`
}

// Image contains information about parsed images on the page
type Image struct {
	URL         string  `json:"url"`
	Type        string  `json:"type"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Alt         string  `json:"alt"`
	AspectRatio float64 `json:"aspectRatio"`
	Preferred   bool    `json:"preferred,omitempty"`
}

type metaTag struct {
	name     string
	value    string
	priority float64
}

type imgTag struct {
	url       string
	alt       string
	preferred bool
}

type parsedImage struct {
	url         string
	data        io.Reader
	alt         string
	contentType string
	preferred   bool
}

var targetedProperties = map[string]float64{
	"og:site_name":   1,
	"og:title":       1,
	"og:type":        1,
	"og:description": 1,
	"og:author":      1,
	"og:publisher":   1,
	"og:url":         1,
	"og:image":       1,

	"site_name":   0.5,
	"title":       0.5,
	"type":        0.5,
	"description": 0.5,
	"author":      0.5,
	"publisher":   0.5,
}

var propertyMap = map[string][]string{
	"URL":         {"og:url"},
	"Site":        {"og:site_name", "site_name"},
	"Title":       {"og:title", "title"},
	"Type":        {"og:type", "type"},
	"Description": {"og:description", "description"},
	"Author":      {"og:author", "author"},
	"Publisher":   {"og:publisher", "publisher"},
}

// OptimalAspectRatio is the target aspect ratio that recon favors when looking at images
var OptimalAspectRatio = 1.91

// DefaultImageLookupTimeout is the maximum amount of time recon will spend downloading and analyzing images
var DefaultImageLookupTimeout = 10 * time.Second

// Parse takes a url and attempts to parse it. This function instanciates a fresh Parser each time it's invoked.
func Parse(url string) (Result, error) {
	p := NewParser()
	return p.Parse(url)
}

// NewParser returns a new Parser object
func NewParser() *Parser {
	p := &Parser{
		client:             getDefaultParserClient(),
		imageLookupTimeout: DefaultImageLookupTimeout,
	}

	return p
}

// WithClient allows the user to specify a custom HTTP client that the parser will use.
func (p *Parser) WithClient(client *http.Client) *Parser {
	p.client = client
	return p
}

// WithImageLookupTimeout allows the user to set the maximum amount of time recon will spend parsing images.
func (p *Parser) WithImageLookupTimeout(t time.Duration) *Parser {
	p.imageLookupTimeout = t
	return p
}

// WithTokenMaxBuffer limits the amount of memory used by the HTML tokenizer.
func (p *Parser) WithTokenMaxBuffer(s int) *Parser {
	p.tokenMaxBuffer = s
	return p
}

// WithHeaders allows the user to set the HTTP request headers
func (p *Parser) WithHeaders(h http.Header) *Parser {
	p.headers = h
	return p
}

// Parse takes a url and attempts to parse it.
func (p *Parser) Parse(url string) (Result, error) {
	job, err := p.getHTML(url)
	if err != nil {
		return Result{}, errors.Wrap(err, "get html")
	}

	if err := job.tokenize(); err != nil {
		return Result{}, errors.Wrap(err, "tokenize")
	}

	imgs := p.analyzeImages(job.requestURL, job.imgTags)
	res := job.buildResult(imgs)

	return res, nil
}

func (p *Parser) newReq(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %s, url: %s", err, url)
	}

	req.Header.Add("User-Agent", "recon (github.com/jimmysawczuk/recon; similar to Facebot, facebookexternalhit/1.1)")
	for k, vv := range p.headers {
		req.Header[k] = vv
	}

	return req, nil
}

func (p *Parser) getHTML(url string) (*parseJob, error) {
	req, err := p.newReq(url)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err == nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		err = errors.New(resp.Status)
	}
	if err != nil {
		return nil, fmt.Errorf("http error: %s, url: %s", err, url)
	}

	result := &parseJob{
		request:        req,
		requestURL:     req.URL,
		response:       resp,
		metaTags:       []metaTag{},
		imgTags:        []imgTag{},
		tokenMaxBuffer: p.tokenMaxBuffer,
	}

	return result, nil
}

func (p *parseJob) tokenize() error {
	decoder := html.NewTokenizer(p.response.Body)
	decoder.SetMaxBuf(p.tokenMaxBuffer)

	for {
		tt := decoder.Next()
		switch tt {
		case html.ErrorToken:
			err := decoder.Err()
			if err == io.EOF {
				return nil
			}
			return err

		case html.SelfClosingTagToken, html.StartTagToken:
			t := decoder.Token()
			switch t.Data {
			case "meta":
				res := parseMeta(t)
				p.metaTags = append(p.metaTags, res)

				if res.name == "og:image" {
					p.imgTags = append(p.imgTags, imgTag{
						url:       res.value,
						preferred: true,
					})
				}

			case "img":
				res := parseImg(t)
				if res.url != "" {
					p.imgTags = append(p.imgTags, res)
				}

			case "title":
				textNode := decoder.Next()
				if textNode == html.TextToken {
					content := decoder.Token()
					res := parseTitle(content)
					p.metaTags = append(p.metaTags, res)
				}
			}
		}
	}
}

func (p *Parser) parseImage(u *url.URL, tag imgTag) (parsedImage, error) {
	req, _ := p.newReq(u.String())
	resp, err := p.client.Do(req)
	if err != nil {
		return parsedImage{}, errors.Wrap(err, "parseImage")
	}

	return parsedImage{
		url:         u.String(),
		contentType: resp.Header.Get("Content-Type"),
		data:        resp.Body,
		alt:         tag.alt,
		preferred:   tag.preferred,
	}, nil
}

func (p *parseJob) buildResult(imgs []Image) Result {
	res := Result{}

	res.URL = p.requestURL.String()
	res.Host = p.requestURL.Host
	if canonicalURLStr := p.getMaxProperty("URL"); canonicalURLStr != "" {
		canonicalURL, err := url.Parse(canonicalURLStr)
		if err == nil {
			res.URL = canonicalURL.String()
			res.Host = canonicalURL.Host
		}
	}

	res.Site = p.getMaxProperty("Site")
	res.Title = p.getMaxProperty("Title")
	res.Type = p.getMaxProperty("Type")
	res.Description = p.getMaxProperty("Description")
	res.Author = p.getMaxProperty("Author")
	res.Publisher = p.getMaxProperty("Publisher")
	res.Images = imgs
	res.Scraped = time.Now()

	return res
}

func (p *parseJob) getMaxProperty(key string) (val string) {
	maxWeight := 0.0

	for _, searchTag := range propertyMap[key] {
		for _, tag := range p.metaTags {
			if tag.name == searchTag && tag.priority > maxWeight {
				val = tag.value
				maxWeight = tag.priority
			}
		}
	}

	return
}

func getDefaultParserClient() *http.Client {
	client := http.DefaultClient
	client.Jar, _ = cookiejar.New(nil)
	return client
}

func parseMeta(t html.Token) metaTag {
	var content string
	var tag string
	var priority float64

	for _, v := range t.Attr {
		if v.Key == "property" || v.Key == "name" {
			if _priority, exists := targetedProperties[v.Val]; exists {
				tag = strings.TrimSpace(v.Val)
				priority = _priority
			}
		} else if v.Key == "content" {
			content = strings.TrimSpace(v.Val)
		}
	}

	if priority > 0 {
		return metaTag{name: tag, value: content, priority: priority}
	}

	return metaTag{}
}

func parseImg(t html.Token) (i imgTag) {
	for _, v := range t.Attr {
		if v.Key == "src" {
			i.url = v.Val
		} else if v.Key == "alt" {
			i.alt = v.Val
		}
	}

	if i.url != "" {
		return
	}

	return imgTag{}
}

func parseImgFromData(i imgTag) (parsedImage, error) {
	// get the image data from the url, decode it
	parts := strings.SplitN(i.url, ";", 2)
	if len(parts) < 2 {
		return parsedImage{}, errors.New("malformed data url")
	}

	header, body := parts[0], parts[1]
	data := strings.Replace(body, "base64,", "", 1)
	full, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return parsedImage{}, err
	}

	parts = strings.SplitN(header, ":", 2)
	contentType := parts[1]

	return parsedImage{
		contentType: contentType,
		data:        bytes.NewBuffer(full),
		url:         i.url,
		alt:         i.alt,
		preferred:   i.preferred,
	}, nil
}

func parseTitle(t html.Token) metaTag {
	return metaTag{name: "title", value: t.Data, priority: 0.5}
}

func (p *Parser) analyzeImages(baseURL *url.URL, tags []imgTag) []Image {
	ch := make(chan parsedImage)
	returned := []Image{}
	numFound := 0

	for _, tag := range tags {
		go func(tag imgTag, ch chan parsedImage) {
			u, err := url.Parse(tag.url)
			if err != nil {
				// malformed image src
				ch <- parsedImage{}
				return
			}
			u = baseURL.ResolveReference(u)

			if strings.HasPrefix(u.String(), "data:") {
				img, err := parseImgFromData(tag)
				if err != nil {
					ch <- parsedImage{}
				}

				ch <- img
				return
			}

			img, err := p.parseImage(u, tag)
			if err != nil {
				ch <- parsedImage{}
				return
			}

			ch <- img
		}(tag, ch)

		numFound++
	}

	if numFound == 0 {
		return returned
	}

	timeOutCh := time.After(p.imageLookupTimeout)
	for {
		select {
		case <-timeOutCh:
			break

		case incoming := <-ch:
			returned = append(returned, incoming.export())
		}

		if len(returned) >= numFound {
			break
		}
	}

	sort.Slice(returned, func(a, b int) bool {
		if returned[a].Preferred && !returned[b].Preferred {
			return true
		}

		if !returned[a].Preferred && returned[b].Preferred {
			return false
		}

		return math.Abs(float64(returned[a].AspectRatio)-OptimalAspectRatio) < math.Abs(float64(returned[b].AspectRatio)-OptimalAspectRatio)
	})

	return returned
}

func (in parsedImage) export() Image {
	out := Image{
		URL:       in.url,
		Alt:       in.alt,
		Preferred: in.preferred,
		Type:      in.contentType,
	}

	switch in.contentType {
	case "image/jpeg":
		img, _ := jpeg.Decode(in.data)
		if img != nil {
			bounds := img.Bounds()
			out.Width = bounds.Max.X
			out.Height = bounds.Max.Y
		}

	case "image/gif":
		img, _ := gif.Decode(in.data)
		if img != nil {
			bounds := img.Bounds()
			out.Width = bounds.Max.X
			out.Height = bounds.Max.Y
		}

	case "image/png":
		img, _ := png.Decode(in.data)
		if img != nil {
			bounds := img.Bounds()
			out.Width = bounds.Max.X
			out.Height = bounds.Max.Y
		}
	}

	if out.Height > 0 {
		out.AspectRatio = float64(out.Width) / float64(out.Height)
	}

	return out
}
