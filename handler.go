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

const (
	InternalErrorJSON = `{ "error":"Internal Error" }`
)

type Handler struct {
	Log logrus.FieldLogger
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !(r.Method == http.MethodGet || r.Method == http.MethodHead) {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	statusCode, body := handleRequest(h, w.Header(), r)
	w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
	w.WriteHeader(statusCode)
	if r.Method == http.MethodGet {
		if _, err := body.WriteTo(w); err != nil {
			h.Log.Error("Body write error: ", err)
		}
	}
}

func handleRequest(h *Handler, header http.Header, r *http.Request) (statusCode int, body *bytes.Buffer) {
	var bodyErr error
	if body, bodyErr = bodyOrErr(h, header, r); bodyErr != nil {
		header.Set("Content-Type", "application/json")
		return handleError(h, bodyErr)
	}
	return http.StatusOK, body

}

func bodyOrErr(h *Handler, header http.Header, r *http.Request) (body *bytes.Buffer, err error) {
	//return bytes.NewBufferString("All is OK!!!"), nil //TODO
	return nil, &handlerError{statusCode: 500, description: "bbbb"} //TODO
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

//log, extract statusCode, marshal error
func handleError(h *Handler, err error) (statusCode int, body *bytes.Buffer) {
	if handlerErr, ok := err.(*handlerError); ok {
		statusCode = handlerErr.statusCode
		if handlerErr.statusCode >= 400 && handlerErr.statusCode < 500 {
			h.Log.WithField("StatusCode", statusCode).Info("Body handle client error: ", handlerErr)
		} else {
			h.Log.WithField("StatusCode", statusCode).Error("Body handle error: ", handlerErr)
		}

		var marshaledData []byte
		marshalError := map[string]string{"error": handlerErr.description}
		if marshaledData, err = json.Marshal(marshalError); err != nil {
			h.Log.Error("handlerErr marshal error: ", err)
			return http.StatusInternalServerError, bytes.NewBufferString(InternalErrorJSON)
		}
		return statusCode, bytes.NewBuffer(marshaledData)
	}
	statusCode = http.StatusInternalServerError
	body = bytes.NewBufferString(InternalErrorJSON)
	h.Log.WithField("StatusCode", statusCode).Error("Body handle error: ", err)
	return
}
