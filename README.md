[![Build Status](https://travis-ci.org/Skipor/imgserver.svg?branch=master)](https://travis-ci.org/Skipor/imgserver) [![GoDoc](https://godoc.org/github.com/Skipor/imgserver?status.png)](https://godoc.org/github.com/Skipor/imgserver) 
# imgserver
Test task for Yandex internship. 
Simple Go server that download all &lt;img> tag images from HTML page, and return page with all images encoded base64. URL of HTTP page passes by query attribute.
Support many different page encodings
# usage
`go get github.com/Skipor/imgserver`

`$GOBIN/imgserver -p 8888`

`curl http://localhost:8888/?url=https://habrahabr.ru/interesting/`

