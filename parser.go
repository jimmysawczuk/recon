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
	"time"

	"image/gif"
	"image/jpeg"
	"image/png"
)

type Parser struct {
	client   *http.Client
	req      *http.Request
	metaTags []metaTag
	imgTags  []imgTag
}

type ParseResult struct {
	URL         string             `json:"url"`
	Host        string             `json:"host"`
	Site        string             `json:"site_name"`
	Title       string             `json:"title"`
	Type        string             `json:"type"`
	Description string             `json:"description"`
	Author      string             `json:"author"`
	Publisher   string             `json:"publisher"`
	Images      []parseResultImage `json:"images"`
	Scraped     time.Time          `json:"scraped"`
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

type parseResultImage struct {
	URL         string  `json:"url"`
	Type        string  `json:"type"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Alt         string  `json:"alt"`
	AspectRatio float64 `json:"aspectRatio"`
	Preferred   bool    `json:"preferred,omitempty"`
}

type byPreferred []parseResultImage

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
	"URL":         []string{"og:url"},
	"Site":        []string{"og:site_name", "site_name"},
	"Title":       []string{"og:title", "title"},
	"Type":        []string{"og:type", "type"},
	"Description": []string{"og:description", "description"},
	"Author":      []string{"og:author", "author"},
	"Publisher":   []string{"og:publisher", "publisher"},
}

var OptimalAspectRatio = 1.91

func NewParser() Parser {
	client := http.DefaultClient
	client.Jar, _ = cookiejar.New(nil)

	return Parser{
		client:   client,
		imgTags:  make([]imgTag, 0),
		metaTags: make([]metaTag, 0),
	}
}

func (p *Parser) Parse(url string) (ParseResult, error) {
	req, err := http.NewRequest("GET", url, nil)

	resp, err := p.client.Do(req)
	if err != nil {
		return ParseResult{}, fmt.Errorf("http error: %s", err)
	}

	p.req = req

	decoder := html.NewTokenizer(resp.Body)
	done := false
	for !done {
		tt := decoder.Next()
		switch tt {
		case html.ErrorToken:
			if decoder.Err() == io.EOF {
				done = true
			} else {
				// fmt.Println(decoder.Err())
			}
		case html.SelfClosingTagToken:
			t := decoder.Token()
			if t.Data == "meta" {
				res := p.parseMeta(t)
				if res.priority > 0 {
					p.metaTags = append(p.metaTags, res)
				}
			} else if t.Data == "img" {
				res := p.parseImg(t)
				if res.url != "" {
					p.imgTags = append(p.imgTags, res)
				}
			}
		}
	}

	res := p.buildResult()

	return res, nil
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
	} else {
		return metaTag{}
	}
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
	} else {
		return imgTag{}
	}
}

func (p *Parser) buildResult() ParseResult {
	res := ParseResult{}
	if canonical_url := p.getMaxProperty("URL"); canonical_url != "" {
		res.URL = canonical_url
	} else {
		res.URL = p.req.URL.String()
	}
	res.Host = p.req.URL.Host

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

func (p *Parser) analyzeImages() []parseResultImage {
	type image struct {
		url          string
		data         io.Reader
		alt          string
		content_type string
		preferred    bool
	}

	ch := make(chan image)
	returned_images := make([]parseResultImage, 0)
	found_images := 0

	for _, tag := range p.imgTags {
		go func(tag imgTag) {
			img_url, _ := url.Parse(tag.url)
			img_url = p.req.URL.ResolveReference(img_url)

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
		}(tag)
		found_images++
	}

	if found_images == 0 {
		return returned_images
	}

	for {
		select {
		case incoming_img := <-ch:
			ret_image := parseResultImage{}
			switch incoming_img.content_type {
			case "image/jpeg":
				img, _ := jpeg.Decode(incoming_img.data)
				bounds := img.Bounds()
				ret_image.Width = bounds.Max.X
				ret_image.Height = bounds.Max.Y

			case "image/gif":
				img, _ := gif.Decode(incoming_img.data)
				bounds := img.Bounds()
				ret_image.Width = bounds.Max.X
				ret_image.Height = bounds.Max.Y

			case "image/png":
				img, _ := png.Decode(incoming_img.data)
				bounds := img.Bounds()
				ret_image.Width = bounds.Max.X
				ret_image.Height = bounds.Max.Y

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

		if len(returned_images) >= len(p.imgTags) {
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
