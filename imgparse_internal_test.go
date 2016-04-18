package imgserver

import (
	"net/url"

	"bytes"

	log "github.com/Sirupsen/logrus"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var _ = func() {
}

var _ = Describe("get folder URL by page URL", func() {
	var (
		pageRawURL string
		pageURL    *url.URL
		res        string
	)
	JustBeforeEach(func() {
		var err error
		pageURL, err = url.Parse(pageRawURL)
		Expect(err).NotTo(HaveOccurred())
		res = getFolderURL(*pageURL)
	})
	Context("when pageURL end with '/'", func() {
		const correctRes = `https://golang.org/doc`
		BeforeEach(func() {
			pageRawURL = "https://golang.org/doc/articles/"
		})
		It("then return non empty value", func() {
			Expect(res).NotTo(BeEmpty())
		})
		It("then return value is correct", func() {
			Expect(res).To(Equal(correctRes))
		})
	})
	Context("when pageURL don't end with '/'", func() {
		const correctRes = `https://golang.org/doc`
		BeforeEach(func() {
			pageRawURL = "https://golang.org/doc/articles"
		})
		It("then return non empty value", func() {
			Expect(res).NotTo(BeEmpty())
		})
		It("then return value is correct", func() {
			Expect(res).To(Equal(correctRes))

		})
	})

})

//func getImgURL(src string, folderURL string) (string, error)
var _ = Describe("getImgURL by src atribute and folder URL", func() {
	var (
		src       string
		folderURL string
		res       string
		err       error
	)
	JustBeforeEach(func() {
		res, err = getImgURL(src, folderURL)
	})

	Context("when image in same folder", func() {
		const correctRes = "https://golang.org/doc/articles/html5.gif"
		BeforeEach(func() {
			src = "html5.gif"
			folderURL = "https://golang.org/doc/articles"
		})
		It("then not error", func() {
			Expect(err).NotTo(HaveOccurred())
		})
		It("then return value is correct", func() {
			Expect(res).To(Equal(correctRes))
		})
	})

	Context("when image in another folder", func() {
		const correctRes = "https://golang.org/doc/images/html5.gif"
		BeforeEach(func() {
			src = "/images/html5.gif"
			folderURL = "https://golang.org/doc"
		})
		It("then not error", func() {
			Expect(err).NotTo(HaveOccurred())
		})
		It("then return value is correct", func() {
			Expect(res).To(Equal(correctRes))
		})
	})

	Context("when image is absolute URL", func() {
		const correctRes = "https://golang.org/doc/images/html5.gif"
		BeforeEach(func() {
			src = "https://golang.org/doc/images/html5.gif"
			folderURL = "https://golang.org/doc"
		})
		It("then not error", func() {
			Expect(err).NotTo(HaveOccurred())
		})
		It("then return value is correct", func() {
			Expect(res).To(Equal(correctRes))
		})
	})

	//when incorrectness is small we handle it on image fetch phase
	Context("when image src absolutely incorrect", func() {
		BeforeEach(func() {
			src = `@@@@@@!@#$%^&*()_@@/*\n!@#$\n\n7asdlfkj/.asdf1#`
			folderURL = "https://golang.org/doc/articles"
		})
		It("then error", func() {
			Expect(err).To(HaveOccurred())
		})
		It("then return value is empty", func() {
			Expect(res).To(BeEmpty())
		})
	})

	//error check
	Context("when image in another folder and '/' on src begin and folder end", func() {
		const correctRes = "https://golang.org/doc/images/html5.gif"
		BeforeEach(func() {
			src = "/images/html5.gif"
			folderURL = "https://golang.org/doc/"
		})
		It("then not error", func() {
			Expect(err).NotTo(HaveOccurred())
		})
		It("then return value is correct", func() {
			Expect(res).To(Equal(correctRes))
		})
	})

	Context("when image in another folder and no '/' on ends", func() {
		const correctRes = "https://golang.org/doc/images/html5.gif"
		BeforeEach(func() {
			src = "images/html5.gif"
			folderURL = "https://golang.org/doc"
		})
		It("then not error", func() {
			Expect(err).NotTo(HaveOccurred())
		})
		It("then return value is correct", func() {
			Expect(res).To(Equal(correctRes))
		})
	})

})

var _ = Describe("working with ImgToken", func() {
	log.SetOutput(GinkgoWriter)
	var (
		tokenData string
		token     html.Token
		img       imgTag
		err       error
	)
	JustBeforeEach(func() {
		z := html.NewTokenizer(bytes.NewBufferString(tokenData))
		z.Next()
		token = z.Token()
		Expect(token.Type).To(Equal(html.StartTagToken))
		Expect(token.Data).To(Equal("img"))
		Expect(token.DataAtom == atom.Img)
		log.WithFields(log.Fields{
			"type":     token.Type,
			"dataAtom": token.DataAtom,
			"data":     token.Data,
			"attr":     token.Attr,
		}).Debug(token.String())
		img, err = parseImgToken(token)
	})

	Context("when img start tag parseImgToken", func() {
		Context("when correct token", func() {
			const (
				srcval = "image.gif"
				altval = "aaaa"
			)
			BeforeEach(func() {
				tokenData = `<img    alt="aaaa" src="image.gif" >`
			})
			It("then not error", func() {
				Expect(err).NotTo(HaveOccurred())
			})
			It("then attributes are correct", func() {
				attr := img.attr
				Expect(attr[0].Key).To(Equal("alt"))
				Expect(attr[0].Val).To(Equal(altval))
				Expect(attr[1].Key).To(Equal("src"))
				Expect(attr[1].Val).To(Equal(srcval))
			})
			It("then src index is correct", func() {
				Expect(img.srcIndex).To(Equal(1))
			})
			It("then img.token() equals origin token", func() {
				Expect(img.token()).To(Equal(token))
			})

			It("then src setter/getter works well", func() {
				Expect(img.src()).To(Equal(srcval))
				img.setSrc("bbb")
				Expect(img.src()).To(Equal("bbb"))
			})
		})

		Context("when token well formated", func() {
			BeforeEach(func() {
				tokenData = `<img alt="aaaa" src="bbbb">`
			})
			It("then token.token().String() equals input data", func() {
				Expect(img.token().String()).To(Equal(tokenData))
			})

		})

		Context("when no src attribute", func() {
			BeforeEach(func() {
				tokenData = `<img    alt="aaaa" >`
			})
			It("then error", func() {
				Expect(err).To(HaveOccurred())
			})
			It("then img is zero", func() {
				Expect(img).To(BeZero())
			})

		})
	})

})
