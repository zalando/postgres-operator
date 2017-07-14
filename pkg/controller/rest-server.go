package controller

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

func (c *Controller) restAPIServer(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	wg.Add(1)

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", c.opConfig.Port),
		Handler: c,
	}

	go func() {
		err := server.ListenAndServe()
		if err != http.ErrServerClosed {
			c.logger.Fatalf("could not start http server: %v", err)
		}
	}()
	c.logger.Infof("listening on %s", server.Addr)

	<-stopCh
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	server.Shutdown(ctx)
}

func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Request: %+v\n", r.URL.String())
}
