package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync"
)

func (c *Controller) RunAPIServer(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	mux := http.NewServeMux()

	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	mux.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", c.opConfig.APIPort),
		Handler: mux,
	}

	go func() {
		err := server.ListenAndServe()
		if err != http.ErrServerClosed {
			c.logger.Fatalf("could not start http server: %v", err)
		}
	}()
	c.logger.Infof("listening on %s", server.Addr)

	<-stopCh

	ctx, _ := context.WithCancel(context.Background())
	server.Shutdown(ctx)
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Request: %+v\n", r.URL.String())
}
