package imgserver_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	logger "github.com/Sirupsen/logrus"
	"github.com/Skipor/imgserver"
)

func TestImgserver(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Imgserver Suite")
}

var logger imgserver.Logger

var _ = BeforeSuite(func() {
	logger.SetLevel(logger.DebugLevel)
	logger.SetOutput(GinkgoWriter)
	logger.SetFormatter(&logger.TextFormatter{})
	logger = logger.StandardLogger()
})

var _ = AfterSuite(func() {

})
