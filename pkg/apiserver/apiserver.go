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

const (
	httpAPITimeout  = time.Minute * 1
	shutdownTimeout = time.Second * 10
	httpReadTimeout = time.Millisecond * 100
)

type ControllerInformer interface {
	GetStatus() interface{}
	ClusterStatus(team, cluster string) interface{}
	ClusterLogs(team, cluster string) interface{}
	TeamClustersStatus(team string) []interface{}
}

type Server struct {
	logger     *logrus.Entry
	http       http.Server
	controller ControllerInformer
}

var (
	clusterStatusURL = regexp.MustCompile("^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/(?P<cluster>[a-zA-Z][a-zA-Z0-9]*)/?$")
	clusterLogsURL   = regexp.MustCompile("^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/(?P<cluster>[a-zA-Z][a-zA-Z0-9]*)/logs/?$")
	teamURL          = regexp.MustCompile("^/clusters/(?P<team>[a-zA-Z][a-zA-Z0-9]*)/?$")
)

func New(controller ControllerInformer, port int, logger *logrus.Logger) *Server {
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
	mux.HandleFunc("/clusters/", s.clusters)

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
			s.logger.Fatalf("Could not start http server: %v", err)
		}
	}()
	s.logger.Infof("Listening on %s", s.http.Addr)

	<-stopCh

	ctx, _ := context.WithTimeout(context.Background(), shutdownTimeout)
	err := s.http.Shutdown(ctx)
	if err == context.DeadlineExceeded {
		s.logger.Warnf("Shutdown timeout exceeded. closing http server")
		s.http.Close()
	} else if err != nil {
		s.logger.Errorf("Could not shutdown http server", err)
	}
	s.logger.Infoln("Http server shut down")
}

func (s *Server) status(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(s.controller.GetStatus())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		s.logger.Errorf("Could not encode status: %v", err)
	}
}

func (s *Server) clusters(w http.ResponseWriter, req *http.Request) {
	var resp interface{}

	if matches := clusterStatusURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		resp = s.controller.ClusterStatus(matches[0][1], matches[0][2])
	} else if matches := teamURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		resp = s.controller.TeamClustersStatus(matches[0][1])
	} else if matches := clusterLogsURL.FindAllStringSubmatch(req.URL.Path, -1); matches != nil {
		resp = s.controller.ClusterLogs(matches[0][1], matches[0][2])
	} else {
		http.NotFound(w, req)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		s.logger.Errorf("Could not list clusters: %v", err)
	}
}
