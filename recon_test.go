package recon

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"testing"
)

func testParse(t *testing.T, url string, local string, confidence float64, expected ParseResult) {
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

	assert.Equal(expected.Title, res.Title)
	assert.Equal(expected.Author, res.Author)
}

func TestParseNYT(t *testing.T) {
	testParse(
		t,
		"http://www.nytimes.com/2015/04/10/arts/television/on-game-of-thrones-season-5-a-change-of-scene.html",
		"test-html/nyt-game-of-thrones.html",
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
		"test-html/jimmysawczuk-2015-mlb-preview.html",
		0,
		ParseResult{
			Title:  `Once more, with feeling`,
			Author: ``,
		},
	)
}

func TestParse538(t *testing.T) {
	testParse(
		t,
		"http://fivethirtyeight.com/datalab/our-33-weirdest-charts-from-2014/",
		"test-html/fivethirtyeight-33-weirdest-charts.html",
		0,
		ParseResult{
			Title: `Our 33 Weirdest Charts From 2014`,
			Site:  `FiveThirtyEight`,
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
