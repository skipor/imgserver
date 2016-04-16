package main

import (
	"fmt"
	"log"
	"net/http"

	logger "github.com/Sirupsen/logrus"
	. "github.com/Skipor/imgserver"
)

const (
	PORT = 8888
)

func init() {
	logger.SetFormatter(&logger.TextFormatter{})
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

func main() {
	l := logger.StandardLogger()

	//set logrus as standart log output
	w := l.Writer()
	defer w.Close()
	log.SetOutput(w)

	imgHandler := &Handler{
		Log:          logger.StandardLogger(),
		LogicHandler: &ImgLogicHandler{l},
		ErrorHandler: &ErrorLogger{l},
	}
	http.Handle("/", rootHandler{imgHandler})
	logger.Fatal(
		http.ListenAndServe(
			fmt.Sprint(":", PORT),
			nil,
		),
	)
}
