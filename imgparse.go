package imgserver

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/context"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type imgTag struct {
	srcIndex int
	attr     []html.Attribute
}

func (img *imgTag) setSrc(src string) {
	img.attr[img.srcIndex].Val = src
}
func (img imgTag) src() string {
	return img.attr[img.srcIndex].Val
}
func (img imgTag) isDataURL() bool {
	return strings.HasPrefix(img.src(), "data:")
}

var supportedImgAttributes = map[string]bool{
	"src":      true,
	"alt":      true,
	"style":    true,
	"longdesc": true,
	"width":    true,
	"height":   true,
}

func (img imgTag) token() html.Token {
	return html.Token{
		html.StartTagToken,
		atom.Img,
		"",
		img.attr,
	}
}

func parseImgToken(token html.Token) (imgTag, error) {
	img := imgTag{-1, make([]html.Attribute, 0, len(token.Attr))}
	for i, attr := range token.Attr {
		key := attr.Key
		if supportedImgAttributes[key] {
			if key == "src" {
				img.srcIndex = i
			}
			img.attr = append(img.attr, token.Attr[i])
		}
	}
	if img.srcIndex < 0 {
		return imgTag{}, NewHandlerError(400, "no src attribute for <img/> tag")
	}
	return img, nil
}

//run goroutine that parse http and goroutine for image download

type imgExtractor interface {
	extractImages(ctx context.Context, r io.Reader) ([]imgTag, error)
}
type imgExtractorFunc func(ctx context.Context, r io.Reader) ([]imgTag, error)

func (f imgExtractorFunc) extractImages(ctx context.Context, r io.Reader) ([]imgTag, error) {
	return f(ctx, r)
}

func extractImages(ctx context.Context, r io.Reader) ([]imgTag, error) {
	ctx, cancel := context.WithCancel(ctx)

	parseResChan, parseErrChan := imageParse(ctx, r)

	// by contract all fetch subrotines should write either to res either to err channel
	fetchResChan := make(chan imgTag)
	fetchErrChan := make(chan error)
	await := 0 //number of fetch routines to await
	result := make([]imgTag, 0)
	//await subroutines on panic or
	defer func() {
		cancel() // cancel subroutines fetch requests
		//await canceled subroutines
		for ; await > 0; await-- {
			select {
			case <-fetchResChan:
			case <-fetchErrChan:
			}
		}
		//for debug close fetch channels
		//leaked fetch subroutine will cause panic on closed channel
		close(fetchResChan)
		close(fetchErrChan)

	}()
	//prepare URL
	pageURL := getURLParam(ctx)
	pageURL.Fragment = ""
	pageURL.RawQuery = ""
	pageURLStr := pageURL.String()

	for parseResChan != nil && await > 0 {
		select {
		case img, closed := <-parseResChan:
			//disable parse channels on parse finish
			if closed {
				parseResChan = nil
				parseErrChan = nil
				continue
			}
			//create new fetch routine on img
			if img.isDataURL() {
				fetchResChan <- img
			}
			imgURL, err := getImgURL(img, pageURLStr)
			if err != nil {
				return nil, &HandlerError{400, "invalid img tag src URL", err}
			}
			await++
			fetchImage(ctx, img, imgURL, fetchResChan, fetchErrChan)
		case err := <-parseErrChan:
			return nil, err
		case fetchedImg := <-fetchResChan:
			await--
			result = append(result, fetchedImg)
		case err := <-fetchErrChan:
			await--
			return nil, err
		}

	}
	return result, nil
}

// try to download parsedImage
func fetchImage(ctx context.Context, img imgTag, imgURL string, imgc chan<- imgTag, errc chan<- error) {
	go func() {
		resp, err := cxtAwareGet(ctx, imgURL)
		if err != nil {
			errc <- &HandlerError{500, "can't fetch image: " + imgURL, err}
			return
		}
		defer resp.Body.Close()
		ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
		if ct == "" {
			errc <- NewHandlerError(400, "no content-type on image: "+imgURL)
			return
		}
		if !strings.HasPrefix(ct, "image") {
			errc <- NewHandlerError(400, "not image content-type on image: "+imgURL)
			return
		}
		dataURLBuf := bytes.NewBufferString("data:")
		dataURLBuf.WriteString(ct)
		dataURLBuf.WriteString(";base64,")

		w := base64.NewEncoder(base64.StdEncoding, dataURLBuf)
		defer w.Close()
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			errc <- &HandlerError{400, "image fetching error: " + imgURL, err}
			return
		}
		resImg := imgTag{ //copy
			img.srcIndex,
			append([]html.Attribute{}, img.attr...),
		}
		resImg.setSrc(dataURLBuf.String())
		imgc <- resImg

	}()
}

func imageParse(ctx context.Context, r io.Reader) (<-chan imgTag, <-chan error) {
	resChan := make(chan imgTag)
	errChan := make(chan error)
	go func() {
		z := html.NewTokenizer(r)
		for {
			tokenType := z.Next()
			if tokenType == html.ErrorToken {
				if z.Err() != io.EOF { //EOF == successful finish
					errChan <- z.Err()
				}
				return
			}
			token := z.Token()
			switch tokenType {
			case html.StartTagToken: // <tag>
				if token.DataAtom != atom.Img {
					continue
				}
				img, err := parseImgToken(token)
				if err != nil {
					errChan <- err
					return
				}
				resChan <- img

			}
		}
	}()
	return resChan, errChan
}

//TODO test
func getImgURL(img imgTag, pageURL string) (string, error) {
	if img.isDataURL() {
		panic(errors.New("imgURL on 'data: URL' image"))
	}
	src := img.src()

	imgSrcURL, err := url.Parse(src)
	if err != nil {
		return "", err
	}
	if imgSrcURL.IsAbs() {
		return src, nil
	}
	if src[0] == '/' {
		return pageURL + src, nil
	}
	return pageURL + "/" + src, nil

}
