package recon

import (
	"github.com/stretchr/testify/assert"

	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testParse(t *testing.T, url string, local string, confidence float64, expected Result) {
	contents, err := ioutil.ReadFile(local)
	if err != nil {
		t.Errorf("Couldn't load test file")
		return
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Errorf("Couldn't create request")
		return
	}

	testResponse := httptest.NewRecorder()
	testResponse.Write(contents)

	intRes := &parseJob{
		request:    req,
		requestURL: req.URL,
		response:   testResponse.Result(),
		metaTags:   []metaTag{},
		imgTags:    []imgTag{},
	}

	err = intRes.tokenize()
	if err != nil {
		t.Errorf("Error tokenizing valid file: %s", err)
		return
	}

	// imgs := p.analyzeImages(intRes.requestURL, intRes.imgTags)
	res := intRes.buildResult([]Image{})

	assert.Equal(t, expected.Title, res.Title, "Titles should match")
	assert.Equal(t, expected.Author, res.Author, "Authors should match")
	assert.Equal(t, expected.Site, res.Site, "Sites should match")
	assert.Equal(t, expected.Description, res.Description, "Descriptions should match")
}

func TestParseNYT(t *testing.T) {
	testParse(
		t,
		"https://www.nytimes.com/2015/04/10/arts/television/on-game-of-thrones-season-5-a-change-of-scene.html",
		"test-html/nyt-game-of-thrones.html",
		0,
		Result{
			Title:       `On ‘Game of Thrones,’ a Change of Scene`,
			Author:      `Mike Hale`,
			Description: `Season 5 of HBO’s fantasy hit based on George R. R. Martin’s novels finds people on the move, on the road and in a couple of picturesque new settings.`,
		},
	)
}

func TestParse538(t *testing.T) {
	testParse(
		t,
		"http://fivethirtyeight.com/datalab/our-33-weirdest-charts-from-2014/",
		"test-html/fivethirtyeight-33-weirdest-charts.html",
		0,
		Result{
			Title:       `Our 33 Weirdest Charts From 2014`,
			Site:        `FiveThirtyEight`,
			Author:      `Andrei Scheinkman`,
			Description: `We've made over 2,000 charts since FiveThirtyEight launched in March. With the new year upon us, here are some of our favorites, with an eye toward forms outside the usual dots, lines and bars.`,
		},
	)
}

func TestParseCNN(t *testing.T) {
	testParse(
		t,
		"http://www.cnn.com/2015/04/14/us/georgia-atlanta-public-schools-cheating-scandal-verdicts/index.html",
		"test-html/cnn-open-tag-test.html",
		0,
		Result{
			Title:       `Prison time for some Atlanta school educators in cheating scandal - CNN.com`,
			Site:        `CNN`,
			Author:      `Ashley Fantz, CNN`,
			Description: `Sparks flew in an Atlanta courtroom Tuesday as educators were sentenced in one of the most massive school cheating scandals in the country.`,
		},
	)
}

func TestParseNoImage(t *testing.T) {
	testParse(
		t,
		"http://localhost/no-img-test.html",
		"test-html/no-img-test.html",
		0,
		Result{
			Title:  `Test`,
			Site:   ``,
			Author: ``,
		},
	)
}

func TestFullParse(t *testing.T) {
	res, err := Parse("https://section411.com/2016/10/running-the-towpath/")

	assert.Nil(t, err)
	assert.Equal(t, "Running the Towpath", res.Title)

	preferredFound := false
	for _, img := range res.Images {
		if img.Preferred {
			preferredFound = true
			assert.Equal(t, "https://cdn.section411.com/h1KaJ7Bl?mw=2048", img.URL)
			assert.Equal(t, 2048.0/1536.0, img.AspectRatio)
		}
	}

	assert.True(t, preferredFound, "didn't find the preferred image")
}

func TestFullParseWithTimeout(t *testing.T) {
	p := NewParser().WithImageLookupTimeout(0 * time.Second)
	res, err := p.Parse("https://section411.com/2016/10/running-the-towpath/")

	assert.Nil(t, err)
	assert.Equal(t, "Running the Towpath", res.Title)
}

func TestErrors(t *testing.T) {
	var err error

	_, err = Parse("")
	assert.NotNil(t, err)

	_, err = Parse("invalid url")
	assert.NotNil(t, err)
}
