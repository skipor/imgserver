package toutf8

import (
	"io"
	"github.com/djimenez/iconv-go"
	"syscall"
)

var (
	UnsuportedCharset error = syscall.EINVAL
	IllegalInputSequence error = syscall.EILSEQ
)

func Decode(dest io.Writer, src io.Reader, charset string) (writen int64, err error) {
	r, err := iconv.NewReader(src, charset, "utf-8")
	if err != nil {
		return 0, err
	}
	return io.Copy(dest, r)
}

