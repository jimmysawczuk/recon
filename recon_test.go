package recon

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"testing"
	"time"
)

func testParse(t *testing.T, url string, local string, confidence float64, expected ParseResult) {
	ImageLookupTimeout = 30 * time.Second

	assert := assert.New(t)

	contents, err := ioutil.ReadFile(local)
	if err != nil {
		t.Errorf("Couldn't load test file")
		return
	}

	buf := bytes.NewBuffer(contents)

	p := NewParser()
	p.reset()

	p.req, err = http.NewRequest("GET", url, nil)
	if err != nil {
		t.Errorf("Couldn't create request")
		return
	}

	err = p.tokenize(buf, 0)
	if err != nil {
		t.Errorf("Error tokenizing valid file: %s", err)
		return
	}

	res := p.buildResult()

	assert.Equal(expected.Title, res.Title, "Titles should match")
	assert.Equal(expected.Author, res.Author, "Authors should match")
	assert.Equal(expected.Site, res.Site, "Sites should match")
}

func TestParseNYT(t *testing.T) {
	testParse(
		t,
		"http://www.nytimes.com/2015/04/10/arts/television/on-game-of-thrones-season-5-a-change-of-scene.html",
		"test-html/nyt-game-of-thrones.html.txt",
		0,
		ParseResult{
			Title:  `On ‘Game of Thrones,’ a Change of Scene`,
			Author: `Mike Hale`,
		},
	)
}

func TestParseJS(t *testing.T) {
	testParse(
		t,
		"https://jimmysawczuk.com/2015/03/once-more-with-feeling.html",
		"test-html/jimmysawczuk-2015-mlb-preview.html.txt",
		0,
		ParseResult{
			Title:  `Once more, with feeling`,
			Author: ``,
			Site:   `Cleveland, Curveballs and Common Sense`,
		},
	)
}

func TestParse538(t *testing.T) {
	testParse(
		t,
		"http://fivethirtyeight.com/datalab/our-33-weirdest-charts-from-2014/",
		"test-html/fivethirtyeight-33-weirdest-charts.html.txt",
		0,
		ParseResult{
			Title:  `Our 33 Weirdest Charts From 2014`,
			Site:   `FiveThirtyEight`,
			Author: `Andrei Scheinkman`,
		},
	)
}

func TestParseCNN(t *testing.T) {
	testParse(
		t,
		"http://www.cnn.com/2015/04/14/us/georgia-atlanta-public-schools-cheating-scandal-verdicts/index.html",
		"test-html/cnn-open-tag-test.html.txt",
		0,
		ParseResult{
			Title:  `Prison time for some Atlanta school educators in cheating scandal - CNN.com`,
			Site:   `CNN`,
			Author: `Ashley Fantz, CNN`,
		},
	)
}

func TestParseNoImage(t *testing.T) {
	testParse(
		t,
		"http://localhost/no-img-test.html",
		"test-html/no-img-test.html.txt",
		0,
		ParseResult{
			Title:  `Test`,
			Site:   ``,
			Author: ``,
		},
	)
}

func TestFullParse(t *testing.T) {
	assert := assert.New(t)

	p := NewParser()
	res, err := p.Parse("https://jimmysawczuk.com/2015/03/once-more-with-feeling.html")

	assert.Equal(err, nil, "err should be nil")
	assert.Equal("Once more, with feeling", res.Title)
}

func TestFullParseWithTimeout(t *testing.T) {
	assert := assert.New(t)

	ImageLookupTimeout = 0 * time.Second

	p := NewParser()
	res, err := p.Parse("https://jimmysawczuk.com/2015/03/once-more-with-feeling.html")

	assert.Equal(err, nil, "err should be nil")
	assert.Equal("Once more, with feeling", res.Title)
}

func TestErrors(t *testing.T) {
	assert := assert.New(t)

	p := NewParser()
	var err error

	_, err = p.Parse("")
	assert.NotNil(err)

	_, err = p.Parse("invalid url")
	assert.NotNil(err)

	_, err = p.ParseWithConfidence("http://www.google.com", -1)
	assert.NotNil(err)
}
