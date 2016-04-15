package imgserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Sirupsen/logrus"
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
	HandleLogic(r *http.Request) (*Response, error)
}

type ErrorHandler interface {
	HandleError(r *http.Request, err error) *Response
}

type Handler struct {
	Log Logger
	LogicHandler LogicHandler
	ErrorHandler ErrorHandler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !(r.Method == http.MethodGet || r.Method == http.MethodHead) {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	resp, err := h.LogicHandler.HandleLogic(r)
	if err != nil {
		resp = h.ErrorHandler.HandleError(r, err)
	}

	for key, valueList := range resp.Header {
		w.Header().Del(key)
		for _ ,value := range valueList {
			w.Header().Add(key, value)
		}
	}

	w.Header().Set("Content-Length", strconv.Itoa(resp.Body.Len()))
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodGet {
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

func (h *ErrorLogger) HandleError(r *http.Request, err error) *Response {
	if hErr, ok := err.(*HandlerError); ok {
		if hErr.statusCode >= 400 && hErr.statusCode < 500 {
			h.Log.WithField("StatusCode", hErr.statusCode).Info("Body handle client error: ", hErr)
		} else {
			h.Log.WithField("StatusCode", hErr.statusCode).Error("Body handle error: ", hErr)
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

func (h *ImgLogicHandler) HandleLogic(r *http.Request) (*Response, error) {
	return nil, &HandlerError{statusCode: 500, description: "bbbb"} //TODO
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

	urlParam := urlParms[0]
	if urlParam == "" {
		return nil, errors.New("url param expected")
	}

	return url.Parse(urlParam)
}
