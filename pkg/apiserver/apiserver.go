package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"regexp"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
)

const httpAPITimeout = time.Second * 30
const shutdownTimeout = time.Second * 30
const httpReadTimeout = time.Millisecond * 100

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
	mux.HandleFunc("/status", s.status)
	mux.HandleFunc("/clusters", s.clusters)

	s.http = http.Server{
		Addr:        fmt.Sprintf(":%d", port),
		Handler:     http.TimeoutHandler(mux, httpAPITimeout, ""),
		ReadTimeout: httpReadTimeout,
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

	ctx, _ := context.WithTimeout(context.Background(), shutdownTimeout)
	err := s.http.Shutdown(ctx)
	if err == context.DeadlineExceeded {
		s.logger.Warnf("shutdown timeout exceeded. closing http server")
		s.http.Close()
	} else if err != nil {
		s.logger.Errorf("could not shutdown http server", err)
	}
	s.logger.Infoln("http server shut down")
}

func (s *Server) status(w http.ResponseWriter, req *http.Request) {
	status := s.controller.Status()

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(status)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		s.logger.Errorf("could not encode status: %v", err)
	}
}

func (s *Server) clusters(w http.ResponseWriter, req *http.Request) {
	var resp interface{}

	if matches := clusterStatusURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		resp = s.controller.ClusterStatus(matches[0][1], matches[0][2])
	} else if matches := teamURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		// TODO
	} else {
		http.NotFound(w, req)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		s.logger.Errorf("could not list clusters: %v", err)
	}
}
