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
	"github.com/Skipor/imgserver/toutf8"
	"github.com/asaskevich/govalidator" //IsUrl
	"golang.org/x/net/context"
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
	Log Logger
}

func (h *ImgLogicHandler) HandleLogic(req *http.Request) (*Response, error) {
	// ctx is the Context for this handler. Calling cancel closes the
	// ctx.Done channel, which is the cancellation signal for requests
	// started by this handler.
	h.Log.Debug("In HandleLogic")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Cancel ctx as soon as handleSearch returns.

	urlParam, err := extractURLParam(req.URL)
	if err != nil {
		return nil, err
	}

	h.Log.Infof("Conten-Type: %s", req.Header.Get("Content-Type"))
	htmlReq, err := http.NewRequest("GET", urlParam.String(), nil)
	if err != nil {
		return nil, &HandlerError{500, "Can't get requested page", err}
	}
	var httpBody *bytes.Buffer

	err = httpDo(ctx, htmlReq, func(resp *http.Response, err error) error {
		if err != nil {
			return err
		}
		httpBody, err = getUTF8HTTPBody(h.Log, resp)
		return err
	})
	if err != nil {
		return nil, err
	}
	header := make(http.Header) //TODO remove
	header.Set("Content-Type", "text/html;charset=utf-8")

	return &Response{200, header, httpBody}, nil
}
func getUTF8HTTPBody(log Logger, resp *http.Response) (*bytes.Buffer, error) {
	var err error
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	typeSubtype, charset := parseContentType(ct)
	log.Infof("typeSubtype: %s", typeSubtype) //todo make debug
	log.Infof("charset: %s", charset) //todo make debug
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

func httpDo(ctx context.Context, req *http.Request, f func(*http.Response, error) error) error {
	// Run the HTTP request in a goroutine and pass the response to f.
	tr := &http.Transport{}
	client := &http.Client{Transport: tr}
	c := make(chan error, 1)
	go func() {
		c <- f(client.Do(req))
	}()
	select {
	case <-ctx.Done():
		tr.CancelRequest(req)
		<-c // Wait for f to return.
		return ctx.Err()
	case err := <-c:
		return err
	}
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
