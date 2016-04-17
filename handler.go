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
	"strings"
	"unicode/utf8"

	"github.com/Sirupsen/logrus"
	"github.com/asaskevich/govalidator" //IsUrl
	"golang.org/x/net/context"

	"github.com/Skipor/imgserver/toutf8"
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
}

func NewImgLogicHandler(log Logger) *ImgLogicHandler {
	tr := &http.Transport{}
	return &ImgLogicHandler{
		log,
		tr,
		&http.Client{Transport:tr},
	}
}

func (h *ImgLogicHandler) HandleLogic(req *http.Request) (*Response, error) {
	h.Log.Debug("In HandleLogic")
	// ctx is the Context for this handler. Calling cancel closes the
	// ctx.Done channel, which is the cancellation signal for requests
	// started by this handler.
	// abstract way to handle cancel and timeouts
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Cancel ctx as soon as handleSearch returns.

	urlParam, err := extractURLParam(req.URL)
	if err != nil {
		return nil, err
	}
	h.Log.Infof("Conten-Type: %s", req.Header.Get("Content-Type"))

	// request will be canceled on context cancel or timeout
	//another way to do context-aware request. Way to set req.Cancel = ctx.Done seems be better
	//resp, err := ctxhttp.Get(ctx, h.client, urlParam.String())
	htmlReq, err := http.NewRequest("GET", urlParam.String(), nil)
	htmlReq.Cancel = ctx.Done()
	if err != nil {
		return nil, &HandlerError{400, "Can't get requested page", err}
	}
	resp, err := h.Client.Do(htmlReq)
	if err != nil {
		return nil, &HandlerError{400, "Can't get requested page", err}
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

type image struct {
	url        *url.URL
	format     string
	base64Data *bytes.Buffer
}

//run goroutine that parse http and goroutine for image download
//func extractImages(ctx *context.Context, body *bytes.Buffer) (<-chan *image, <-chan error)

//func parseHTTP

func getUTF8HTTPBody(ctx context.Context, h *ImgLogicHandler, resp *http.Response) (*bytes.Buffer, error) {
	log := h.Log
	var err error
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	typeSubtype, charset := parseContentType(ct)
	log.Debugf("typeSubtype: %s", typeSubtype)
	log.Debugf("charset: %s", charset)
	if typeSubtype != "text/html" {
		return nil, NewHandlerError(400, "illegal content-type on requested page: "+ct)
	}
	bodyBuf := &bytes.Buffer{}
	_, err = io.Copy(bodyBuf, resp.Body)
	if err != nil {
		return nil, &HandlerError{500, "Can't get body of requested page", err}
	}
	const utf8Charset = "utf-8"
	//return body if it utf-8 encoded already
	if charset == utf8Charset || charset == strings.ToUpper(utf8Charset) || charset == "" && utf8.Valid(bodyBuf.Bytes()) {
		return bodyBuf, nil
	}
	if charset == "" {
		charset = "ISO-8859-1" //default HTTP charset
	}
	decodedBodyBuf := &bytes.Buffer{}
	_, err = toutf8.Decode(decodedBodyBuf, bodyBuf, charset)
	if err == toutf8.UnsuportedCharset {
		_, err = toutf8.Decode(decodedBodyBuf, bodyBuf, strings.ToLower(charset)) //another try can works sometime
	}
	//TODO try parse <meta> html tag for encoding
	switch err {
	case nil:
		return decodedBodyBuf, nil
	case toutf8.UnsuportedCharset:
		return nil, &HandlerError{400, "Requested page have unsupported charset: " + charset, err}
	case toutf8.IllegalInputSequence:
		return nil, &HandlerError{400, "Requested page body has invalid charset sequence", err}
	}
	if !utf8.Valid(decodedBodyBuf.Bytes()) { // TODO remove before release
		return nil, NewHandlerError(500, "body of requested page decoded into invalid utf-8")
	}
	return decodedBodyBuf, nil
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
