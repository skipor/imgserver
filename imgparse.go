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
	attr map[string]string
}

func (img imgTag) isDataURL() bool {
	return strings.HasPrefix(img.attr["src"], "data:")
}

type fetchedImgTag struct {
	imgTag
	//bellow fields can be zero if isDataURL() == true
	format     string
	base64Data *bytes.Buffer
}

//run goroutine that parse http and goroutine for image download

type imgExtractor interface {
	extractImages(ctx context.Context, r io.Reader) ([]fetchedImgTag, error)
}

func extractImages(ctx context.Context, r io.Reader) ([]fetchedImgTag, error) {
	ctx, cancel := context.WithCancel(ctx)

	parseResChan, parseErrChan := imageParse(ctx, r)

	// by contract all fetch subrotines should write either to res either to err channel
	fetchResChan := make(chan fetchedImgTag)
	fetchErrChan := make(chan error)
	await := 0 //number of fetch routines to await
	result := make([]fetchedImgTag, 0)
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
		case parsedImgPtr := <-parseResChan:
			//disable parse channels on parse finish
			if parsedImgPtr == nil {
				parseResChan = nil
				parseErrChan = nil
				continue
			}
			//create new fetch routine on img
			await++

			img := *parsedImgPtr
			if img.isDataURL() {
				fetchResChan <- fetchedImgTag{*parsedImgPtr, "", nil}
			}
			imgURLStr, err := imgURL(img, pageURLStr)
			if err != nil {
				return nil, err
			}
			fetchImage(ctx, img, imgURLStr, fetchResChan, fetchErrChan)
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

//TODO test
func imgURL(img imgTag, pageURL string) (string, error) {
	if img.isDataURL() {
		panic(errors.New("imgURL on 'data: URL' image"))
	}
	src := img.attr["src"]

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

func imageParse(ctx context.Context, r io.Reader) (<-chan *imgTag, <-chan error) {
	resChan := make(chan *imgTag)
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

var supportedImgAttributes = map[string]bool{
	"src":      true,
	"alt":      true,
	"style":    true,
	"longdesc": true,
	"width":    true,
	"height":   true,
}

func parseImgToken(token html.Token) (*imgTag, error) {
	img := &imgTag{}
	for _, attr := range token.Attr {
		if supportedImgAttributes[attr.Key] {
			img.attr[attr.Key] = attr.Val
		}
	}
	if img.attr["src"] == "" {
		return nil, NewHandlerError(400, "no src attribute for <img/> tag")
	}
	return img, nil

}

// try to download parsedImage
func fetchImage(ctx context.Context, img imgTag, imgURL string, imgc chan<- fetchedImgTag, errc chan<- error) {
	if img.isDataURL() {
		panic(errors.New("imgURL on 'data: URL' image"))
	}
	go func() {
		resp, err := cxtAwareGet(ctx, imgURL)
		if err != nil {
			errc <- &HandlerError{500, "can't fetch image: " + imgURL, err}
			return
		}
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			errc <- NewHandlerError(400, "no content-type on image: "+imgURL)
			return
		}
		b64buf := &bytes.Buffer{}
		w := base64.NewEncoder(base64.StdEncoding, b64buf)
		defer w.Close()
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			errc <- &HandlerError{400, "image fetching error: " + imgURL, err}
			return
		}
		imgc <- fetchedImgTag{img, ct, b64buf}

	}()
}
