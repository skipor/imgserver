package imgserver

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/asaskevich/govalidator"

	"sync"
	"time"

	"github.com/cenk/backoff"
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
		Type:     html.StartTagToken,
		DataAtom: atom.Img,
		Data:     "img",
		Attr:     img.attr,
	}
}

//run goroutine that parse http and goroutine for image download

type imgExtractor interface {
	//read html data from r and return all <img> tags converted to data:URL form
	extractImages(ctx context.Context, r io.Reader) ([]imgTag, error)
}
type imgExtractorFunc func(ctx context.Context, r io.Reader) ([]imgTag, error)

func (f imgExtractorFunc) extractImages(ctx context.Context, r io.Reader) ([]imgTag, error) {
	return f(ctx, r)
}

type imgExtractorImp struct {
	parser  imageParser
	fetcher imageFetcher
}

func (imp imgExtractorImp) extractImages(ctx context.Context, r io.Reader) ([]imgTag, error) {
	log := getLocalLogger(ctx, "extractImages")
	log.Debug("Extracting images")
	ctx, cancel := context.WithCancel(ctx)
	parseResChan, parseErrChan := imp.parser.parseImage(ctx, r)

	// by contract all fetch subrotines should write either to res either to err channel
	fetchResChan := make(chan imgTag)
	fetchErrChan := make(chan error)
	await := 0 //number of fetch routines to await
	var result []imgTag
	//await subroutines on panic or
	defer func() {
		cancel() // cancel subroutines fetch requests
		//await canceled subroutines
		log.WithField("fetchToAwait", await).Debug("awaiting")
		for ; await > 0; await-- {
			select {
			case <-fetchResChan:
				log.Debug("img awaited")
			case <-fetchErrChan:
				log.Debug("error awaited")
			}
		}
		//for debug close fetch channels
		//leaked fetch subroutine will cause panic on closed channel
		close(fetchResChan)
		close(fetchErrChan)

	}()
	folderURL := *getFolderURL(*getURLParam(ctx))

	log.Debug("Async await")
	//while parsing in process and fetch tasks not finished
	for parseResChan != nil || await > 0 {
		select {
		case img, ok := <-parseResChan:
			log.Debug("Async got image")
			if !ok {
				//check if parse finish successful
				log.Debug("parse finished succesfuly")
				//disable parse channels on parse finish
				parseResChan = nil
				parseErrChan = nil
				continue
			}
			//create new fetch routine on img
			if img.isDataURL() {
				log.Debug("img with data URL parsed")
				result = append(result, img)
				continue
			}
			imgURL, err := getImgURL(img.src(), folderURL)
			if err != nil {
				return nil, err
			}
			log.WithField("token", img.token().String()).
				Debug("img parsed. Send for fetching")
			await++
			log.Debug("Async fetching image")
			imp.fetcher.fetchImage(ctx, img, imgURL, fetchResChan, fetchErrChan)
		case err := <-parseErrChan:
			log.Debug("parse finished with error")
			return nil, err
		case img := <-fetchResChan:
			log.Debug("img fetched")
			await--
			result = append(result, img)
		case err := <-fetchErrChan:
			log.Debug("error on img fetch")
			await--
			return nil, err
		}

	}
	log.Debug("Async await Done")
	return result, nil
}

type imageFetcher interface {
	// try to download parsedImage
	// on success send only img to imgc
	// on fail send only error to errc
	fetchImage(ctx context.Context, img imgTag, imgURL string, imgc chan<- imgTag, errc chan<- error)
}
type imageFetcherFunc func(ctx context.Context, img imgTag, imgURL string, imgc chan<- imgTag, errc chan<- error)

func (f imageFetcherFunc) fetchImage(ctx context.Context, img imgTag, imgURL string, imgc chan<- imgTag, errc chan<- error) {
	f(ctx, img, imgURL, imgc, errc)
}

func fetchImage(ctx context.Context, img imgTag, imgURL string, imgc chan<- imgTag, errc chan<- error) {
	go func() {
		resp, err := cxtAwareGet(ctx, imgURL)
		if err != nil {
			errc <- &HandlerError{500, "can't fetch image: " + imgURL, err}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			errc <- NewHandlerError(400, fmt.Sprintf("expected status code 200 but found %v on image: %v )", resp.StatusCode, imgURL))
			return
		}
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

type backoffImageFetcher struct {
	backoffLock *sync.Mutex
	backoff     backoff.BackOff
}

func (h backoffImageFetcher) NextBackOff() time.Duration {
	h.backoffLock.Lock()
	res := h.backoff.NextBackOff()
	h.backoffLock.Unlock()
	return res
}
func (h backoffImageFetcher) Reset() {
	h.backoffLock.Lock()
	h.backoff.Reset()
	h.backoffLock.Unlock()
}

func (bif backoffImageFetcher) fetchImage(ctx context.Context, img imgTag, imgURL string, imgc chan<- imgTag, errc chan<- error) {
	go func() {
		log := getLocalLogger(ctx, "backoffFetcher")
		var (
			opErr  error
			opImg * imgTag
		)
		operation := func() error {
			// return err on retry need, or just returns
			log.Debug("Another try")
			//TODO remove code duplication
			resp, err := cxtAwareGet(ctx, imgURL)
			if err != nil {
				log.Debug("Get error")
				opErr = &HandlerError{500, "can't fetch image: " + imgURL, err}
				return nil
			}
			defer resp.Body.Close()

			if resp.StatusCode >= 500 {
				//retry on server error
				log.Debug("Got server error response -> do next try")
				return &HandlerError{500, "need retry", nil}
			}

			if resp.StatusCode != http.StatusOK {
				opErr = NewHandlerError(400, fmt.Sprintf("expected status code 200 but found %v on image: %v )", resp.StatusCode, imgURL))
				return nil
			}
			ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
			if ct == "" {
				opErr = NewHandlerError(400, "no content-type on image: "+imgURL)
				return nil
			}
			if !strings.HasPrefix(ct, "image") {
				opErr = NewHandlerError(400, "not image content-type on image: "+imgURL)
				return nil
			}
			dataURLBuf := bytes.NewBufferString("data:")
			dataURLBuf.WriteString(ct)
			dataURLBuf.WriteString(";base64,")

			w := base64.NewEncoder(base64.StdEncoding, dataURLBuf)
			defer w.Close()
			_, err = io.Copy(w, resp.Body)
			if err != nil {
				opErr = &HandlerError{400, "image fetching error: " + imgURL, err}
				return nil
			}
			opImg = &imgTag{ //copy
				img.srcIndex,
				append([]html.Attribute{}, img.attr...),
			}
			opImg.setSrc(dataURLBuf.String())
			return nil
		}
		//err := backoff.Retry(operation, bif)
		err := backoff.Retry(operation, backoff.NewExponentialBackOff())
		if err != nil {
			errc <- err
		} else {
			if opErr != nil {
				errc <- err
			} else {
				imgc <- img
			}

		}
	}()
}

type imageParser interface {
	//parse html content in separate goroutine and send imgTags to output img chan
	//img chan will be closed on parse finish
	//on parse error, parser send err to error chan before finish
	//err chan is unbuffered
	parseImage(ctx context.Context, r io.Reader) (<-chan imgTag, <-chan error)
}
type imageParserFunc func(ctx context.Context, r io.Reader) (<-chan imgTag, <-chan error)

func (f imageParserFunc) parseImage(ctx context.Context, r io.Reader) (<-chan imgTag, <-chan error) {
	return f(ctx, r)
}

type imageParserImp struct {
	tokenParse imgTokenParser
}

//TODO test
func (imp imageParserImp) parseImage(ctx context.Context, r io.Reader) (<-chan imgTag, <-chan error) {
	imgc := make(chan imgTag)
	errc := make(chan error)
	go func() {
		// on error, error is send before deffer, so receiver got error, and then close signal
		defer func() {
			close(imgc)
		}() // indicate finish
		z := html.NewTokenizer(r)
		for {
			tokenType := z.Next()
			if tokenType == html.ErrorToken {
				if z.Err() != io.EOF {
					//EOF == successful finish
					errc <- z.Err() //block until receiver got error
				}
				return
			}
			token := z.Token()
			switch tokenType {
			case html.SelfClosingTagToken:
				fallthrough
			case html.StartTagToken: // <tag>
				if token.DataAtom != atom.Img || token.Data != "img" {
					continue
				}

				img, err := imp.tokenParse.parseImgToken(token)
				if err != nil {
					errc <- err
					return
				}
				imgc <- img

			}
		}
	}()
	return imgc, errc
}

type imgTokenParser interface {
	parseImgToken(token html.Token) (imgTag, error)
}
type imgTokenParserFunc func(token html.Token) (imgTag, error)

func (f imgTokenParserFunc) parseImgToken(token html.Token) (imgTag, error) {
	return f(token)
}

func parseImgToken(token html.Token) (imgTag, error) {
	img := imgTag{-1, make([]html.Attribute, 0, len(token.Attr))}
	for i, attr := range token.Attr {
		key := attr.Key
		if supportedImgAttributes[key] {
			if key == "src" {
				img.srcIndex = len(img.attr)
			}
			img.attr = append(img.attr, token.Attr[i])
		}
	}
	if img.srcIndex < 0 {
		return imgTag{}, NewHandlerError(400, "no src attribute for <img/> tag")
	}
	return img, nil
}

func getFolderURL(pageURL url.URL) *url.URL {
	pageURL.Fragment = ""
	pageURL.RawQuery = ""
	pageURL.Path = strings.Trim(pageURL.Path, "/")
	if !strings.ContainsRune(pageURL.Path, '/') {
		pageURL.Path = ""
	} else {
		split := strings.Split(pageURL.Path, "/")
		pageURL.Path = strings.Join(split[:len(split)-1], "/")
	}
	return &pageURL
}

func getImgURL(src string, folderURL url.URL) (string, error) {
	imgSrcURL, err := url.Parse(src)
	if err != nil {
		return "", &HandlerError{400, "invalid img tag src URL: url parse", err}
	}
	var res string
	if imgSrcURL.IsAbs() {
		res = src
	} else if strings.HasPrefix(src, "//") {
		//yep, is absolute too
		res = "http:" + src
	} else if src[0] == '/' {
		folderURL.Path = src
		folderURL.RawPath = src
		res = folderURL.String()
	} else {
		folderStr := strings.TrimRight(folderURL.String(), "/") //caulse relative
		if folderStr[len(folderStr)-1] == '/' {
			res = folderStr + src
		} else {
			res = folderStr + "/" + src
		}
	}
	if !govalidator.IsURL(res) {
		return "", &HandlerError{400, "invalid img tag src URL: is not valid URL", err}
	}
	return res, nil

}
