package imgserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/context"
	"golang.org/x/net/html/charset"

	"github.com/Sirupsen/logrus"
	"github.com/asaskevich/govalidator" //IsUrl
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
	h.Log.Debugf("Content-Type: %s", req.Header.Get("Content-Type"))
	resp, err := cxtAwareGet(ctx, h.Client, urlParam.String())
	if err != nil {
		return nil, &HandlerError{500, "Can't get requested page", err}
	}
	httpBody, err := getUTF8HTTPBody(ctx, resp)
	if err != nil {
		return nil, err
	}

	{ //TODO change with real logic
		header := make(http.Header)
		header.Set("Content-Type", "text/html;charset=utf-8")
		return &Response{200, header, httpBody}, nil
	}
}

func getUTF8HTTPBody(ctx context.Context, resp *http.Response) (*bytes.Buffer, error) {
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
