package imgserver

import (
	"net/http"

	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"
)

func cxtAwareGet(ctx context.Context, cl *http.Client, URL string) (*http.Response, error) {
	// request will be canceled on context cancel or timeout
	return ctxhttp.Get(ctx, cl, URL)

	// another way to do context-aware request.
	// Way to set req.Cancel = ctx.Done seems have better performance, but return not ctx.Err() on ctx.Done
	//req, err := http.NewRequest("GET", reqUrl, nil)
	//req.Cancel = ctx.Done()
	//if err != nil {
	//	return nil, &HandlerError{500, "Can't get requested page", err}
	//}
	//return h.Client.Do(req)
}
