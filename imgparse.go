package imgserver

import (
	"bytes"
	"encoding/base64"
	"io"

	"golang.org/x/net/context"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type imgTag struct {
	attr map[string]string
}

type fetchedImgTag struct {
	imgTag
	format     string
	base64Data *bytes.Buffer
}

//run goroutine that parse http and goroutine for image download
func extractImages(ctx context.Context, h *ImgLogicHandler, r io.Reader) ([]fetchedImgTag, error) {
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
			//TODO URL composition
			fetchImage(ctx, h, *parsedImgPtr, fetchResChan, fetchErrChan)
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

// type Token struct {
//     Type     TokenType
//     DataAtom atom.Atom
//     Data     string
//     Attr     []Attribute
// }
//
// type Attribute struct {
//     Namespace, Key, Val string
// }

// try to download parsedImage
func fetchImage(ctx context.Context, h *ImgLogicHandler, img imgTag, imgc chan<- fetchedImgTag, errc chan<- error) {
	go func() {
		src := img.attr["src"]
		resp, err := cxtAwareGet(ctx, h.Client, src)
		if err != nil {
			errc <- &HandlerError{500, "can't fetch image: " + src, err}
			return
		}
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			errc <- NewHandlerError(400, "no content-type on image: "+src)
			return
		}
		b64buf := &bytes.Buffer{}
		w := base64.NewEncoder(base64.StdEncoding, b64buf)
		defer w.Close()
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			errc <- &HandlerError{400, "image fetching error: " + src, err}
			return
		}
		imgc <- fetchedImgTag{img, ct, b64buf}

	}()
}
