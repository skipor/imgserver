package main

import (
	"fmt"
	stdlog "log"
	"net/http"
	"os"

	logger "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"

	. "github.com/Skipor/imgserver"
)

const (
	port = 8888
)

var log = logger.StandardLogger()

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

func mainAction(c *cli.Context) {
	imgHandler := NewImgCtxAdaptor(log, http.DefaultClient)
	http.Handle("/", rootHandler{imgHandler})

	port := c.Int("port")
	if !(port > 0 && port < 65536) {
		log.Fatalf("Invalid port given")
	}

	log.Infof("Listening port :%v", port)
	log.Fatal(
		http.ListenAndServe(
			fmt.Sprint(":", port),
			nil,
		),
	)

}

func main() {
	//set logrus as stdlog output
	w := log.Writer()
	defer w.Close()
	stdlog.SetOutput(w)

	app := cli.NewApp()
	app.Version = "0.0.1"
	app.Name = "imgserv"
	app.Usage = "listen http requests with ?url query param and send response with page of data:URL encoded images"
	app.Flags = []cli.Flag{
		cli.IntFlag{
			Name:  "port, p",
			Value: 8888,
			Usage: "listen port",
		},
	}
	app.Action = mainAction
	app.Run(os.Args)
}
