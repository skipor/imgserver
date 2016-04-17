package main

import (
	"fmt"
	stdlog "log"
	"net/http"

	log "github.com/Sirupsen/logrus"
	. "github.com/Skipor/imgserver"
	"golang.org/x/net/context"
)

const (
	PORT = 8888
)

func init() {
	log.SetFormatter(&log.TextFormatter{})
}

type rootHandler struct {
	h http.Handler
}

func (h rootHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	h.h.ServeHTTP(w, r)
}

//TODO pass port by flag
func main() {
	logger := log.StandardLogger()

	//set logrus as standart log output
	w := logger.Writer()
	defer w.Close()
	stdlog.SetOutput(w)

	imgHandler := ContextAdaptor{
		Handler: &ImgHandler{
			Log:          logger,
			LogicHandler: NewImgLogicHandler(logger, http.DefaultClient),
			ErrorHandler: &ErrorLogger{logger},
		},
		Ctx: context.Background(),
	}
	http.Handle("/", rootHandler{imgHandler})
	logger.Fatal(
		http.ListenAndServe(
			fmt.Sprint(":", PORT),
			nil,
		),
	)
}
