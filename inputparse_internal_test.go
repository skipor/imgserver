package imgserver

import (
	"net/url"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parameter Parse", func() {
	var (
		inputRawURL string
		res         *url.URL
		err         error
	)
	JustBeforeEach(func() {
		parsedURL, inputParseErr := url.Parse(inputRawURL)
		Expect(parsedURL).NotTo(BeNil())
		Expect(inputParseErr).NotTo(HaveOccurred())

		res, err = extractURLParam(parsedURL)
	})

	Context("when correct input", func() {
		const CorrectParameterValue = `https://golang.org/doc/`
		BeforeEach(func() {
			inputRawURL = "http://localhost:8888/?url=https%3A%2F%2Fgolang.org%2Fdoc%2F"
		})
		It("then no error", func() {
			Expect(err).ToNot(HaveOccurred())
		})
		It("then return non nil value", func() {
			Expect(res).NotTo(BeNil())
		})
		It("then return value equals to passed", func() {
			correctParsed, err := url.Parse(CorrectParameterValue)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(correctParsed))
		})
	})

	Context("when unexpexted parameter name", func() {
		BeforeEach(func() {
			inputRawURL = "http://localhost:8888/?qwerty=https%3A%2F%2Fgolang.org%2Fdoc%2F"
		})
		It("then error", func() {
			Expect(err).To(HaveOccurred())
		})
		It("then return nil", func() {
			Expect(res).To(BeNil())
		})
	})

	Context("when no parameters", func() {
		BeforeEach(func() {
			inputRawURL = "http://localhost:8888/"
		})
		It("then error", func() {
			Expect(err).To(HaveOccurred())
		})
		It("then return nil", func() {
			Expect(res).To(BeNil())
		})
	})
	Context("when more than one input parameters", func() {
		BeforeEach(func() {
			inputRawURL = "http://localhost:8888/?url=https%3A%2F%2Fgolang.org%2Fdoc%2F&url=https%3A%2F%2Fgolang.org%2Fdoc%2F"
		})
		It("then error", func() {
			Expect(err).To(HaveOccurred())
		})
		It("then return nil", func() {
			Expect(res).To(BeNil())
		})
	})

	Context("when another parameter on same name", func() {
		BeforeEach(func() {
			inputRawURL = "http://localhost:8888/?url=https%3A%2F%2Fgolang.org%2Fdoc%2F&noturl=https%3A%2F%2Fgolang.org%2Fdoc%2F"
		})
		It("then error", func() {
			Expect(err).To(HaveOccurred())
		})
		It("then return nil", func() {
			Expect(res).To(BeNil())
		})
	})

	Context("when invalid URL as parameter", func() {
		BeforeEach(func() {
			inputRawURL = "http://localhost:8888/?url=qwertyqwerty"
		})
		It("then error", func() {
			Expect(err).To(HaveOccurred())
		})
		It("then return nil", func() {
			Expect(res).To(BeNil())
		})
	})
	Context("when relative URL as parameter", func() {
		BeforeEach(func() {
			inputRawURL = "http://localhost:8888/?url=%2Fqwertyqwerty"
		})
		It("then error", func() {
			Expect(err).To(HaveOccurred())
		})
		It("then return nil", func() {
			Expect(res).To(BeNil())
		})
	})
	Context("when  URL has fragment parameter", func() {
		BeforeEach(func() {
			inputRawURL = url.QueryEscape(`?url=https://golang.org/doc/#abc`)
		})
		It("then error", func() {
			Expect(err).To(HaveOccurred())
		})
		It("then return nil", func() {
			Expect(res).To(BeNil())
		})
	})
})

var _ = Describe("Parse Content-Type", func() {
	var (
		contentType string
		typeSubtype string
		charset string
	)

	JustBeforeEach(func() {
		typeSubtype, charset = parseContentType(contentType)
	})

	Context("when content-type have no charset", func() {
		BeforeEach(func() {
			contentType = `  application/x-www-form-urlencoded  `
		})
		It("then type/subteype same as passed", func() {
			Expect(typeSubtype).To(Equal("application/x-www-form-urlencoded"))
		})
		It("then charset is empty", func() {
			Expect(charset).To(BeZero())
		})
	})

	Context("when content-type have charset", func() {
		BeforeEach(func() {
			contentType = `  text/html ; charset=utf-8 `
		})
		It("then type/subteype same as passed", func() {
			Expect(typeSubtype).To(Equal("text/html"))
		})
		It("then charset same as passed", func() {
			Expect(charset).To(Equal("utf-8"))
		})
	})

	Context("when content-type have  invalid charset", func() {
		BeforeEach(func() {
			contentType = ` text/html; charset =utf-8 `
		})
		It("then type/subteype is empty", func() {
			Expect(typeSubtype).To(BeZero())
		})
		It("then charset same is empty", func() {
			Expect(charset).To(BeZero())
		})
	})
	Context("when content-type have  invalid type/subtype", func() {
		BeforeEach(func() {
			contentType = ` text/ html ; charset=utf-8 `
		})
		It("then type/subteype is empty", func() {
			Expect(typeSubtype).To(BeZero())
		})
		It("then charset same is empty", func() {
			Expect(charset).To(BeZero())
		})
	})

})
