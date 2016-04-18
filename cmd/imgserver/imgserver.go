package main

import (
	"fmt"
	stdlog "log"
	"net/http"
	"os"

	logger "github.com/Sirupsen/logrus"
	. "github.com/Skipor/imgserver"
	"golang.org/x/net/context"
)

const (
	port = 8888
)

func init() {
	logger.SetFormatter(&logger.TextFormatter{})
	logger.SetLevel(logger.DebugLevel)
	logger.SetOutput(os.Stderr)
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
	//set logrus as stdlog output
	log := logger.StandardLogger()
	w := log.Writer()
	defer w.Close()
	stdlog.SetOutput(w)

	imgHandler := ContextAdaptor{
		Handler: &ImgHandler{
			Log:          log,
			LogicHandler: NewImgLogicHandler(log, http.DefaultClient),
			ErrorHandler: ErrorLogger{},
		},
		Ctx: context.Background(),
	}
	http.Handle("/", rootHandler{imgHandler})
	log.Fatal(
		http.ListenAndServe(
			fmt.Sprint(":", port),
			nil,
		),
	)
}
