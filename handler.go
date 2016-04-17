package imgserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"unicode/utf8"

	"github.com/Sirupsen/logrus"
	"github.com/asaskevich/govalidator" //IsUrl
	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"

	"encoding/base64"

	"strings"

	"golang.org/x/net/html/charset"
)

type Logger interface {
	logrus.FieldLogger
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       *bytes.Buffer
}

func NewResponse() *Response {
	return &Response{
		-1,
		make(http.Header),
		&bytes.Buffer{},
	}
}

type LogicHandler interface {
	HandleLogic(req *http.Request) (*Response, error)
}

type ErrorHandler interface {
	HandleError(req *http.Request, err error) *Response
}

type Handler struct {
	Log          Logger
	LogicHandler LogicHandler
	ErrorHandler ErrorHandler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !(req.Method == http.MethodGet || req.Method == http.MethodHead) {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	resp, err := h.LogicHandler.HandleLogic(req)
	if err != nil {
		resp = h.ErrorHandler.HandleError(req, err)
	}

	for key, valueList := range resp.Header {
		w.Header().Del(key)
		for _, value := range valueList {
			w.Header().Add(key, value)
		}
	}

	w.Header().Set("Content-Length", strconv.Itoa(resp.Body.Len()))
	w.WriteHeader(resp.StatusCode)
	if req.Method == http.MethodGet {
		if _, err := resp.Body.WriteTo(w); err != nil {
			h.Log.Error("Body write error: ", err)
		}
	}
}

type ErrorLogger struct {
	Log Logger
}

func NewInternalErrorResponse() *Response {
	return &Response{
		http.StatusInternalServerError,
		http.Header(map[string][]string{
			"Content-Type": []string{"application/json"},
		}),
		bytes.NewBufferString(`{ "error":"Internal Error" }`),
	}
}

func (h *ErrorLogger) HandleError(req *http.Request, err error) *Response {
	//TODO handle http.context errors
	if hErr, ok := err.(*HandlerError); ok {
		if hErr.statusCode >= 400 && hErr.statusCode < 500 {
			h.Log.WithField("StatusCode", hErr.statusCode).Info("Body handle client error: ", hErr)
		} else {
			h.Log.WithField("StatusCode", hErr.statusCode).Warn("Body handle error: ", hErr)
		}

		resp := NewResponse()
		resp.StatusCode = hErr.statusCode
		resp.Header.Set("Content-Type", "application/json")
		marshalError := map[string]string{"error": hErr.description}
		if err = json.NewEncoder(resp.Body).Encode(marshalError); err != nil {
			h.Log.Error("handlerErr marshal error: ", err)
			return NewInternalErrorResponse()
		}
		return resp
	}

	resp := NewInternalErrorResponse()
	h.Log.WithField("StatusCode", resp.StatusCode).Error("Body handle error: ", err)
	return resp
}

type ImgLogicHandler struct {
	Log       Logger
	Transport *http.Transport // use one transport for reusing TCP connections
	Client    *http.Client    // default client for this handler requests
	//TODO make dependency injection for helper functions
}

func NewImgLogicHandler(log Logger) *ImgLogicHandler {
	tr := &http.Transport{}
	return &ImgLogicHandler{
		log,
		tr,
		&http.Client{Transport: tr},
	}
}

//TODO make context handler and impl http.Handler via context adaptor
func (h *ImgLogicHandler) HandleLogic(req *http.Request) (*Response, error) {
	h.Log.Debug("In HandleLogic")
	// ctx is the Context for this handler. Calling cancel closes the
	// ctx.Done channel, which is the cancellation signal for requests
	// started by this handler.
	// abstract way to handle cancel and timeouts

	ctx, cancel := context.WithCancel(context.Background())
	//ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Millisecond * 10)) //TODO just for test
	defer cancel() // Cancel ctx as soon as HandleLogic returns

	urlParam, err := extractURLParam(req.URL)
	if err != nil {
		return nil, err
	}
	h.Log.Infof("Conten-Type: %s", req.Header.Get("Content-Type"))
	resp, err := contextAwareGet(ctx, h, urlParam.String())
	if err != nil {
		return nil, &HandlerError{500, "Can't get requested page", err}
	}
	httpBody, err := getUTF8HTTPBody(ctx, h, resp)
	if err != nil {
		return nil, err
	}

	{ //TODO change with real logic
		header := make(http.Header)
		header.Set("Content-Type", "text/html;charset=utf-8")
		return &Response{200, header, httpBody}, nil
	}
}

type imgTag struct {
	url    string
	altTag string
}

type fetchedImgTag struct {
	imgTag
	format     string
	base64Data *bytes.Buffer
}

//run goroutine that parse http and goroutine for image download
func extractImages(ctx context.Context, h *ImgLogicHandler, body *bytes.Buffer) ([]fetchedImgTag, error) {
	ctx, cancel := context.WithCancel(ctx)

	parseResChan, parseErrChan := imageParse(ctx, h, body)

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

func imageParse(ctx context.Context, h *ImgLogicHandler, htmlData *bytes.Buffer) (<-chan *imgTag, <-chan error) {
	resChan := make(chan *imgTag)
	errChan := make(chan error)
	go func() {

	}()
	return resChan, errChan
}

// try to download parsedImage
func fetchImage(ctx context.Context, h *ImgLogicHandler, img imgTag, imgc chan<- fetchedImgTag, errc chan<- error) {
	go func() {
		resp, err := contextAwareGet(ctx, h, img.url)
		if err != nil {
			errc <- &HandlerError{500, "can't fetch image: " + img.url, err}
			return
		}
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			errc <- NewHandlerError(400, "no content-type on image: "+img.url)
			return
		}
		b64buf := &bytes.Buffer{}
		w := base64.NewEncoder(base64.StdEncoding, b64buf)
		defer w.Close()
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			errc <- &HandlerError{400, "image fetching error: " + img.url, err}
			return
		}
		imgc <- fetchedImgTag{img, ct, b64buf}

	}()
}

func contextAwareGet(ctx context.Context, h *ImgLogicHandler, reqUrl string) (*http.Response, error) {
	// request will be canceled on context cancel or timeout
	return ctxhttp.Get(ctx, h.Client, reqUrl)

	// another way to do context-aware request.
	// Way to set req.Cancel = ctx.Done seems have better performance, but return not ctx.Err() on ctx.Done
	//req, err := http.NewRequest("GET", reqUrl, nil)
	//req.Cancel = ctx.Done()
	//if err != nil {
	//	return nil, &HandlerError{500, "Can't get requested page", err}
	//}
	//return h.Client.Do(req)
}

func getUTF8HTTPBody(ctx context.Context, h *ImgLogicHandler, resp *http.Response) (*bytes.Buffer, error) {
	var err error
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	var ctWithoutParameter string
	if prefix := strings.Split(ct, ";"); len(prefix) != 0 {
		ctWithoutParameter = prefix[0]
	} else {
		ctWithoutParameter = ct
	}
	ctWithoutParameter = strings.TrimSpace(ctWithoutParameter)
	if ctWithoutParameter != "text/html" {
		return nil, NewHandlerError(400, "requested page have unsupported content type")
	}
	r, err := charset.NewReader(resp.Body, ct)
	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, r)
	if err != nil {
		return nil, &HandlerError{400, "Requested page have unsupported charset or invalid charset sequence", err}
	}
	if !utf8.Valid(buf.Bytes()) { // TODO remove before release
		return nil, NewHandlerError(500, "body of requested page decoded into invalid utf-8")
	}
	return buf, nil
}

// try to decode data into utf-8 using iconv
// return number of bytes writen to
// see iconv documentation for error meaning
var contentTypeRegex = regexp.MustCompile(`^\s*([^;\s]+(?:\/[^\/;\s]+))\s*(?:;\s*(?:charset=(?:([^"\s]+)|"([^"\s]+)"))){0,1}\s*$`)

func parseContentType(ct string) (typeSubtype string, parameterValue string) {
	m := contentTypeRegex.FindStringSubmatch(ct)
	if m == nil {
		return "", ""
	}
	if len(m) > 2 {
		return m[1], m[2]
	}
	return m[1], ""
}

func extractURLParam(requestURL *url.URL) (*url.URL, error) {
	query := requestURL.Query()

	const expectedParamsNum = 1
	if len(query) != expectedParamsNum {
		return nil, errors.New("unexpected non url params")
	}

	urlParms := query["url"]
	const expectedURLParams = 1
	if len(urlParms) > expectedURLParams {
		return nil, errors.New("too many url params")
	}
	if len(urlParms) < expectedURLParams {
		return nil, errors.New("too few url params")
	}

	urlParam := urlParms[0]

	if !govalidator.IsURL(urlParam) {
		return nil, NewHandlerError(400, "invalid URL as 'url' query parameter")
	}

	return url.Parse(urlParam)
}
