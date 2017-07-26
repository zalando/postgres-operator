package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync"
	"regexp"

	"github.com/Sirupsen/logrus"
	"encoding/json"
)

type ClusterInformer interface {
	Status() interface{}
	ClusterStatus(team, cluster string) interface{}
}

type Server struct {
	logger     *logrus.Entry
	http       http.Server
	controller ClusterInformer
}

var (
	clusterStatusURL = regexp.MustCompile("^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/(?P<cluster>[a-zA-Z][a-zA-Z0-9]*)/?$")
	teamURL          = regexp.MustCompile("^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/?$")
)

func HandlerFunc(i interface{}) http.HandlerFunc {
	b, err := json.Marshal(i)
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Could not marshal %T: %v", i, err)
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}
}

func New(controller ClusterInformer, port int, logger *logrus.Logger) *Server {
	s := &Server{
		logger:     logger.WithField("pkg", "apiserver"),
		controller: controller,
	}

	mux := http.NewServeMux()

	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	mux.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
	mux.Handle("/status", HandlerFunc(s.controller.Status()))
	mux.HandleFunc("/clusters/", func(w http.ResponseWriter, req *http.Request) {
		var resp interface{}
		if matches := clusterStatusURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
			resp = s.controller.ClusterStatus(matches[0][1], matches[0][2])
		} else if matches := teamURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
			// TODO
		} else {
			http.NotFound(w, req)
			return
		}

		b, err := json.Marshal(resp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Could not marshal %T: %v", resp, err)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Write(b)
		}
	})

	s.http = http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return s
}

func (s *Server) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	go func() {
		err := s.http.ListenAndServe()
		if err != http.ErrServerClosed {
			s.logger.Fatalf("could not start http server: %v", err)
		}
	}()
	s.logger.Infof("listening on %s", s.http.Addr)

	<-stopCh

	ctx, _ := context.WithCancel(context.Background())
	s.http.Shutdown(ctx)
}
