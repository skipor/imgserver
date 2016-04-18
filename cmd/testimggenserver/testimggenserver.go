package main

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
)

//TODO move handler to separate package

func encodeImg(img image.Image, format string) (*bytes.Buffer, error) {
	buff := &bytes.Buffer{}
	var err error
	switch format {
	case "png":
		err = png.Encode(buff, img)
	case "gif":
		err = gif.Encode(buff, img, &gif.Options{256, nil, nil})
	case "jpg", "jpeg":
		err = jpeg.Encode(buff, img, &jpeg.Options{jpeg.DefaultQuality})
	default:
		return nil, errors.New("unexpected format: " + format)
	}
	if err != nil {
		return nil, err
	}
	return buff, nil
}

//generates image with salt injected into image data
//salt can be nil
func generateImg(height int, width int, salt []byte) image.Image {
	if height < 1 || width < 1 {
		panic("Illegal imgage size")
	}
	//make gradient
	img := image.NewNRGBA(image.Rectangle{Min: image.ZP, Max: image.Point{width - 1, height - 1}})
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dark := uint8((float64(height-y)/float64(height) + float64(width-x)/float64(width)) * 128)
			img.SetNRGBA(x, y, color.NRGBA{dark, dark, dark, 255})
		}
	}
	//hash salt into color
	c := color.RGBA{0, 0, 0, 255}
	for i := 2; i < len(salt); i++ {
		c.R ^= salt[i-2]
		c.G ^= salt[i-1]
		c.B ^= salt[i]
	}
	saltImg := &image.Uniform{c}
	bounds := image.Rect(width/4, height/4, width*3/4, height*3/4)
	draw.Draw(img, bounds, saltImg, image.ZP, draw.Src)
	return img
}

func getSalt(path string) []byte {
	hash := crypto.SHA512.New()
	_, err := io.Copy(hash, bytes.NewBufferString(path))
	if err != nil {
		panic(err)
	}
	return hash.Sum(nil)
}

var validPathRegex = regexp.MustCompile(`^\/(?:[a-zA-Z0-9_]+\/)*([1-9][0-9]{0,3})x([1-9][0-9]{0,3})\.(gif|png|jpg|jpeg)$`)

func imgHandle(w http.ResponseWriter, r *http.Request) {
	if !(r.Method == http.MethodGet) {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Path
	// TODO make caching for images
	// it is too low performance for benchmark tests now
	log.Print("Path: ", path)

	matches := validPathRegex.FindStringSubmatch(path)
	if matches == nil {
		http.NotFound(w, r)
		log.Print("invalid path")
		return
	}
	log.Print("matches", matches)

	var height, width int
	var err error
	if height, err = strconv.Atoi(matches[1]); err != nil {
		log.Panic(errors.New("unexpected height"))
	}
	if width, err = strconv.Atoi(matches[2]); err != nil {
		log.Panic(errors.New("unexpected height"))
	}
	format := matches[3]

	var buff *bytes.Buffer
	buff, err = encodeImg(
		generateImg(height, width, getSalt(path)),
		format,
	)
	if err != nil {
		log.Panic("image encode error")
	}
	w.Header().Set("Content-Length", strconv.Itoa(buff.Len()))

	if format == "jpg" {
		format = "jpeg" //there is no image/jpg content-type
	}
	w.Header().Set("Content-Type", "image/"+format)
	_, err = buff.WriteTo(w)
	if err != nil {
		log.Panic("image send error: ", err)
	}
}

func main() {
	const PORT = 8080
	http.HandleFunc("/", imgHandle)
	log.Fatal(
		http.ListenAndServe(
			fmt.Sprint(":", PORT),
			nil,
		),
	)
}
