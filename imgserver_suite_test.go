package imgserver_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestImgserver(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Imgserver Suite")
}
