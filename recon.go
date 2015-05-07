// Package recon scrapes URLs for OpenGraph information.
package recon

import (
	"fmt"
	"golang.org/x/net/html"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"time"

	"image/gif"
	"image/jpeg"
	"image/png"
)

// Parser is the client object and holds the relevant information needed when parsing a URL
type Parser struct {
	client   *http.Client
	req      *http.Request
	metaTags []metaTag
	imgTags  []imgTag
}

// ParseResult is what comes back from a Parse
type ParseResult struct {
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

type byPreferred []Image

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

// ImageLookupTimeout is the maximum amount of time recon will spend downloading and analyzing images
var ImageLookupTimeout = 10 * time.Second

// NewParser returns a new Parser object
func NewParser() Parser {
	p := Parser{}
	return p
}

func (p *Parser) reset() {
	client := http.DefaultClient
	client.Jar, _ = cookiejar.New(nil)

	p.client = client
	p.imgTags = make([]imgTag, 0)
	p.metaTags = make([]metaTag, 0)
}

// Parse takes a url and attempts to parse it using a default minimum confidence of 0 (all information is used).
func (p *Parser) Parse(url string) (ParseResult, error) {
	return p.ParseWithConfidence(url, 0)
}

// ParseWithConfidence takes a url and attempts to parse it, only considering information with a confidence above the minimum confidence provided (should be between 0 and 1).
func (p *Parser) ParseWithConfidence(url string, confidence float64) (ParseResult, error) {
	if confidence < 0 || confidence > 1 {
		return ParseResult{}, fmt.Errorf("ParseWithConfidence: confidence should between 0 and 1 inclusive, is %d", confidence)
	}

	p.reset()
	resp, err := p.doRequest(url)
	if err != nil {
		return ParseResult{}, err
	}

	p.tokenize(resp.Body, confidence)

	res := p.buildResult()

	return res, nil
}

func (p *Parser) doRequest(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %s, url: %s", err, url)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http error: %s, url: %s", err, url)
	}

	p.req = req

	return resp, nil
}

func (p *Parser) tokenize(body io.Reader, confidence float64) error {
	decoder := html.NewTokenizer(body)
	done := false
	for !done {
		tt := decoder.Next()
		switch tt {
		case html.ErrorToken:
			if decoder.Err() == io.EOF {
				done = true
			}

		case html.SelfClosingTagToken, html.StartTagToken:
			t := decoder.Token()
			if t.Data == "meta" {
				res := p.parseMeta(t)
				if res.priority > confidence {
					p.metaTags = append(p.metaTags, res)
				}
			} else if t.Data == "img" {
				res := p.parseImg(t)
				if res.url != "" {
					p.imgTags = append(p.imgTags, res)
				}
			} else if t.Data == "title" {
				text_node := decoder.Next()
				if text_node == html.TextToken {
					content := decoder.Token()
					res := p.parseTitle(content)
					if res.priority > confidence {
						p.metaTags = append(p.metaTags, res)
					}
				}
			}
		}
	}

	return nil
}

func (p *Parser) parseMeta(t html.Token) metaTag {
	var content string
	var tag string
	var priority float64

	for _, v := range t.Attr {
		if v.Key == "property" || v.Key == "name" {
			if _priority, exists := targetedProperties[v.Val]; exists {
				tag = v.Val
				priority = _priority
			}
		} else if v.Key == "content" {
			content = v.Val
		}
	}

	if priority > 0 {
		return metaTag{name: tag, value: content, priority: priority}
	}

	return metaTag{}
}

func (p *Parser) parseImg(t html.Token) (i imgTag) {
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

func (p *Parser) parseTitle(t html.Token) metaTag {
	return metaTag{name: "title", value: t.Data, priority: 0.5}
}

func (p *Parser) buildResult() ParseResult {
	res := ParseResult{}

	res.URL = p.req.URL.String()
	res.Host = p.req.URL.Host
	if canonical_url_str := p.getMaxProperty("URL"); canonical_url_str != "" {
		canonical_url, err := url.Parse(canonical_url_str)
		if err == nil {
			res.URL = canonical_url.String()
			res.Host = canonical_url.Host
		}
	}

	res.Site = p.getMaxProperty("Site")
	res.Title = p.getMaxProperty("Title")
	res.Type = p.getMaxProperty("Type")
	res.Description = p.getMaxProperty("Description")
	res.Author = p.getMaxProperty("Author")
	res.Publisher = p.getMaxProperty("Publisher")

	for _, tag := range p.metaTags {
		if tag.name == "og:image" {
			p.imgTags = append(p.imgTags, imgTag{
				url:       tag.value,
				preferred: true,
			})
		}
	}

	res.Images = p.analyzeImages()

	res.Scraped = time.Now()
	return res
}

func (p *Parser) getMaxProperty(key string) (val string) {
	max_priority := 0.0

	for _, search_tag := range propertyMap[key] {
		for _, tag := range p.metaTags {
			if tag.name == search_tag && tag.priority > max_priority {
				val = tag.value
				max_priority = tag.priority
			}
		}
	}

	return
}

func (p *Parser) analyzeImages() []Image {
	type image struct {
		url          string
		data         io.Reader
		alt          string
		content_type string
		preferred    bool
	}

	ch := make(chan image)
	returned_images := make([]Image, 0)
	found_images := 0

	for _, tag := range p.imgTags {
		img_url, _ := url.Parse(tag.url)
		img_url = p.req.URL.ResolveReference(img_url)

		// TODO: actually handle data urls
		if strings.HasPrefix(img_url.String(), "data:") {
			continue
		}

		go func(img_url *url.URL, tag imgTag) {
			req, _ := http.NewRequest("GET", img_url.String(), nil)
			resp, _ := p.client.Do(req)

			img := image{
				url:          img_url.String(),
				content_type: resp.Header.Get("Content-Type"),

				data: resp.Body,

				alt:       tag.alt,
				preferred: tag.preferred,
			}

			ch <- img
		}(img_url, tag)

		found_images++
	}

	if found_images == 0 {
		return returned_images
	}

	timed_out := false
	timed_out_ch := time.After(ImageLookupTimeout)
	for {
		select {
		case <-timed_out_ch:
			timed_out = true

		case incoming_img := <-ch:
			ret_image := Image{}
			switch incoming_img.content_type {
			case "image/jpeg":
				img, _ := jpeg.Decode(incoming_img.data)
				if img != nil {
					bounds := img.Bounds()
					ret_image.Width = bounds.Max.X
					ret_image.Height = bounds.Max.Y
				}

			case "image/gif":
				img, _ := gif.Decode(incoming_img.data)
				if img != nil {
					bounds := img.Bounds()
					ret_image.Width = bounds.Max.X
					ret_image.Height = bounds.Max.Y
				}

			case "image/png":
				img, _ := png.Decode(incoming_img.data)
				if img != nil {
					bounds := img.Bounds()
					ret_image.Width = bounds.Max.X
					ret_image.Height = bounds.Max.Y
				}
			}

			if ret_image.Height > 0 {
				ret_image.AspectRatio = round(float64(ret_image.Width)/float64(ret_image.Height), 1e-4)
			}

			ret_image.Type = incoming_img.content_type
			ret_image.URL = incoming_img.url
			ret_image.Alt = incoming_img.alt
			ret_image.Preferred = incoming_img.preferred

			returned_images = append(returned_images, ret_image)
		}

		if timed_out {
			break
		}

		if len(returned_images) >= found_images {
			break
		}
	}

	sort.Sort(byPreferred(returned_images))

	return returned_images
}

func (t byPreferred) Less(a, b int) bool {
	if t[a].Preferred && !t[b].Preferred {
		return true
	}

	if !t[a].Preferred && t[b].Preferred {
		return false
	}

	a_diff := math.Abs(t[a].AspectRatio - OptimalAspectRatio)
	b_diff := math.Abs(t[b].AspectRatio - OptimalAspectRatio)

	return a_diff < b_diff
}

func (t byPreferred) Swap(a, b int) {
	t[a], t[b] = t[b], t[a]
}

func (t byPreferred) Len() int {
	return len(t)
}

func round(a float64, prec float64) float64 {
	return math.Floor(a/prec+0.5) * prec
}
