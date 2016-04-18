package imgserver_test

import (
	"testing"

	logger "github.com/Sirupsen/logrus"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/Skipor/imgserver"
)

func TestImgserver(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Imgserver Suite")
}

var log imgserver.Logger

var _ = BeforeSuite(func() {
	logger.SetLevel(logger.DebugLevel)
	logger.SetOutput(GinkgoWriter)
	logger.SetFormatter(&logger.TextFormatter{})
	log = logger.StandardLogger()
	//TODO launch imggen handler for integrate tests

	//SetDefaultEventuallyTimeout(t time.Duration)
	//SetDefaultEventuallyPollingInterval(t time.Duration)
	//SetDefaultConsistentlyDuration(t time.Duration)
	//SetDefaultConsistentlyPollingInterval(t time.Duration)
})

var _ = AfterSuite(func() {

})
